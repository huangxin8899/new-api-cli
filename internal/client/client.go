// Package client 封装对 New API 管理接口的 HTTP 调用。
//
// New API 的管理接口有两个必须处理的特点：
//
//  1. 业务失败也返回 HTTP 200，靠 body 里的 success:false 区分，
//     所以只看状态码会把失败当成功；
//  2. 列表接口统一返回 {items, page, page_size, total} 分页对象。
//
// Do 负责前者，Paginate 负责后者。
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/huangxin8899/new-api-cli/errs"
	"github.com/huangxin8899/new-api-cli/internal/build"
	"github.com/huangxin8899/new-api-cli/internal/config"
)

// Client 是一个绑定了 profile 设置的 New API 管理接口客户端。
type Client struct {
	settings *config.Settings
	http     *http.Client
	// Verbose 为 true 时把请求摘要写到 Log。
	Verbose bool
	// Log 承载调试输出，永远不是 stdout。
	Log io.Writer
}

// New 依据已解析的设置构造客户端。
func New(s *config.Settings) *Client {
	timeout := time.Duration(s.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if s.Insecure {
		// 仅供自签名证书的私有部署；由 --insecure / profile 显式开启。
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		settings: s,
		http:     &http.Client{Timeout: timeout, Transport: transport},
		Log:      io.Discard,
	}
}

// Settings 返回底层设置，命令层用于展示 base URL 等。
func (c *Client) Settings() *config.Settings { return c.settings }

// Request 描述一次管理接口调用。
type Request struct {
	Method string
	// Path 是相对站点根的路径，如 /api/channel/ ；会自动补 /api 前缀。
	Path string
	// Query 是查询参数。
	Query url.Values
	// Body 是要序列化成 JSON 的请求体；与 RawBody 二选一。
	Body any
	// RawBody 是已经序列化好的请求体。
	RawBody []byte
	// Header 是额外请求头。
	Header http.Header
	// NoAuth 跳过 Authorization 头（登录、状态查询等公开接口）。
	NoAuth bool
	// Cookies 附加请求 Cookie（refresh 流程用）。
	Cookies []*http.Cookie
}

// Response 是一次调用的结果。
type Response struct {
	HTTPStatus int
	Success    bool
	Message    string
	// Data 是 New API 信封里的 data 字段原文。
	Data json.RawMessage
	// Body 是完整响应原文，非 JSON 响应（如导出文件）时使用。
	Body []byte
	// Header 是响应头，refresh 流程需要读 Set-Cookie。
	Header http.Header
}

// Decode 把 data 字段反序列化到 v。
func (r *Response) Decode(v any) error {
	if len(r.Data) == 0 || string(r.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(r.Data, v); err != nil {
		return errs.NewAPIError(errs.SubtypeBadResponse,
			"响应数据结构与预期不符: %v", err)
	}
	return nil
}

// Any 把 data 解析成通用 JSON 值，供输出层直接渲染。
func (r *Response) Any() (any, error) {
	if len(r.Data) == 0 || string(r.Data) == "null" {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(r.Data, &v); err != nil {
		return nil, errs.NewAPIError(errs.SubtypeBadResponse,
			"响应不是合法 JSON: %v", err)
	}
	return v, nil
}

// newAPIEnvelope 是 New API 管理接口的统一响应结构。
type newAPIEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Code    any             `json:"code"`
	Data    json.RawMessage `json:"data"`
}

// Do 发起一次调用并把 New API 的失败信封翻译成类型化错误。
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if err := c.settings.RequireBaseURL(); err != nil {
		return nil, err
	}
	if !req.NoAuth {
		if err := c.settings.RequireToken(); err != nil {
			return nil, err
		}
	}

	httpReq, err := c.build(ctx, req)
	if err != nil {
		return nil, err
	}

	resp, err := c.send(httpReq, req)
	if err != nil {
		return nil, err
	}
	return c.interpret(resp, req)
}

// DoRaw 发起调用但不解析信封，用于下载类接口。
func (c *Client) DoRaw(ctx context.Context, req Request) (*Response, error) {
	if err := c.settings.RequireBaseURL(); err != nil {
		return nil, err
	}
	httpReq, err := c.build(ctx, req)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(httpReq, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errs.NewNetworkError("", "读取响应失败: %v", err)
	}
	return &Response{HTTPStatus: resp.StatusCode, Body: body, Header: resp.Header, Success: resp.StatusCode < 300}, nil
}

func (c *Client) build(ctx context.Context, req Request) (*http.Request, error) {
	fullURL, err := c.resolveURL(req.Path, req.Query)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	var payload []byte
	switch {
	case req.RawBody != nil:
		payload = req.RawBody
	case req.Body != nil:
		payload, err = json.Marshal(req.Body)
		if err != nil {
			return nil, errs.NewInternalError("序列化请求体失败: %v", err)
		}
	}
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}

	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, errs.NewValidationError(errs.SubtypeInvalidArgument,
			"构造请求失败: %v", err)
	}

	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "new-api-cli/"+build.Version)
	if payload != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	// New API 对 cookie 认证的接口做同源校验；CLI 是第一方客户端，
	// 声明自己的 Origin 等于目标站点即可通过。
	httpReq.Header.Set("Origin", c.settings.BaseURL)
	if !req.NoAuth && c.settings.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.settings.Token)
	}
	if c.settings.UserID > 0 {
		httpReq.Header.Set("New-API-User", strconv.Itoa(c.settings.UserID))
	}
	for k, values := range req.Header {
		for _, v := range values {
			httpReq.Header.Add(k, v)
		}
	}
	for _, ck := range req.Cookies {
		httpReq.AddCookie(ck)
	}
	return httpReq, nil
}

// resolveURL 把相对路径拼成完整 URL，并自动补 /api 前缀。
func (c *Client) resolveURL(path string, query url.Values) (string, error) {
	base, err := url.Parse(c.settings.BaseURL)
	if err != nil || base.Host == "" {
		return "", errs.NewConfigError(errs.SubtypeConfigCorrupt,
			"站点地址不是合法 URL: %q", c.settings.BaseURL).
			WithHint("形如 https://api.example.com").
			WithParams("--base-url")
	}
	p := NormalizePath(path)
	ref, err := url.Parse(p)
	if err != nil {
		return "", errs.NewValidationError(errs.SubtypeInvalidArgument,
			"路径不合法: %q", path)
	}
	full := base.ResolveReference(ref)
	if len(query) > 0 {
		q := full.Query()
		for k, values := range query {
			for _, v := range values {
				q.Add(k, v)
			}
		}
		full.RawQuery = q.Encode()
	}
	return full.String(), nil
}

// NormalizePath 归一化用户给的路径：
// 支持完整 URL、以 /api 开头的路径、以及省略 /api 的简写。
// /v1 (relay 接口) 与 /pg 等非 /api 前缀路径保持原样。
func NormalizePath(raw string) string {
	p := strings.TrimSpace(raw)
	if idx := strings.Index(p, "://"); idx >= 0 {
		if u, err := url.Parse(p); err == nil {
			p = u.RequestURI()
		}
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	for _, prefix := range []string{"/api/", "/v1/", "/pg/", "/mj/", "/suno/"} {
		if strings.HasPrefix(p, prefix) || p == strings.TrimSuffix(prefix, "/") {
			return p
		}
	}
	return "/api" + p
}

func (c *Client) send(httpReq *http.Request, req Request) (*http.Response, error) {
	// 只有幂等方法自动重试：POST 重试可能造成重复创建渠道/令牌。
	maxAttempts := 1
	if httpReq.Method == http.MethodGet || httpReq.Method == http.MethodHead {
		maxAttempts = 3
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if c.Verbose {
			fmt.Fprintf(c.Log, "[http] %s %s (第 %d 次)\n", httpReq.Method, httpReq.URL.String(), attempt)
		}
		resp, err := c.http.Do(httpReq)
		if err != nil {
			lastErr = c.networkError(err)
			if attempt < maxAttempts && retriableNetErr(err) {
				if !sleepCtx(httpReq.Context(), backoff(attempt)) {
					return nil, lastErr
				}
				continue
			}
			return nil, lastErr
		}
		if attempt < maxAttempts && retriableStatus(resp.StatusCode) {
			resp.Body.Close()
			if !sleepCtx(httpReq.Context(), backoff(attempt)) {
				break
			}
			continue
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = errs.NewNetworkError(errs.SubtypeServerError, "请求重试多次仍未成功")
	}
	return nil, lastErr
}

func (c *Client) interpret(resp *http.Response, req Request) (*Response, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, errs.NewNetworkError("", "读取响应失败: %v", err)
	}

	out := &Response{HTTPStatus: resp.StatusCode, Body: body, Header: resp.Header}

	var env newAPIEnvelope
	jsonOK := json.Unmarshal(body, &env) == nil

	// 先看 HTTP 层：认证中间件用 401/403 表达身份问题。
	if resp.StatusCode >= 400 {
		return out, c.httpError(resp, env, jsonOK, body, req)
	}
	if !jsonOK {
		return out, errs.NewAPIError(errs.SubtypeBadResponse,
			"响应不是合法 JSON（HTTP %d）", resp.StatusCode).
			WithHint("确认 --base-url 指向 New API 站点而非其他服务；响应开头：%s", snippet(body))
	}

	out.Success = env.Success
	out.Message = env.Message
	out.Data = env.Data

	// 再看业务层：New API 的失败也返回 HTTP 200。
	if !env.Success {
		return out, c.businessError(env, req)
	}
	return out, nil
}

func (c *Client) httpError(resp *http.Response, env newAPIEnvelope, jsonOK bool, body []byte, req Request) error {
	message := strings.TrimSpace(env.Message)
	if !jsonOK || message == "" {
		message = fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		if len(body) > 0 && !jsonOK {
			message += "：" + snippet(body)
		}
	}
	code, _ := env.Code.(string)

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		e := errs.NewAuthError(errs.SubtypeTokenInvalid, "%s", message)
		if code == "AUTH_TOKEN_EXPIRED" {
			e.Subtype = errs.SubtypeTokenExpired
		}
		return e.WithHint("令牌来自 %s；执行 new-api-cli auth login 重新登录", c.settings.TokenSource)
	case http.StatusForbidden:
		return errs.NewAuthError(errs.SubtypeForbidden, "%s", message).
			WithHint("当前账号权限不足；管理接口需要管理员（role>=10），系统设置需要超级管理员（role=100）")
	case http.StatusNotFound:
		return errs.NewAPIError(errs.SubtypeNotFound, "%s", message).
			WithHint("确认路径 %s 存在于该 New API 版本；用 new-api-cli api GET <path> 探测", req.Path)
	case http.StatusTooManyRequests:
		return errs.NewAPIError(errs.SubtypeRateLimited, "%s", message).
			WithHint("触发了站点限流，稍后重试或降低并发")
	}
	if resp.StatusCode >= 500 {
		return errs.NewAPIError(errs.SubtypeServerError, "%s", message).
			WithHint("New API 服务端错误，检查站点日志")
	}
	return errs.NewAPIError("", "%s", message).WithCode(resp.StatusCode)
}

func (c *Client) businessError(env newAPIEnvelope, req Request) error {
	message := strings.TrimSpace(env.Message)
	if message == "" {
		message = "接口返回失败但未提供原因"
	}
	e := errs.NewAPIError("", "%s", message)
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(message, "不存在") || strings.Contains(lower, "not found") ||
		strings.Contains(message, "未找到") || strings.Contains(lower, "record not found"):
		e.Subtype = errs.SubtypeNotFound
	case strings.Contains(message, "无权") || strings.Contains(message, "权限") ||
		strings.Contains(lower, "permission") || strings.Contains(lower, "forbidden"):
		return errs.NewAuthError(errs.SubtypeForbidden, "%s", message)
	case strings.Contains(message, "登录") || strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "token") && strings.Contains(lower, "invalid"):
		return errs.NewAuthError(errs.SubtypeTokenInvalid, "%s", message).
			WithHint("执行 new-api-cli auth login 重新登录")
	}
	if id, ok := env.Code.(string); ok && id != "" {
		e.Subtype = strings.ToLower(id)
	}
	return e
}

func (c *Client) networkError(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errs.NewNetworkError(errs.SubtypeTimeout,
			"连接 %s 超时", c.settings.BaseURL).
			WithHint("用 --timeout 增大超时时间，或检查网络连通性").Wrap(err)
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "certificate") || strings.Contains(msg, "x509"):
		return errs.NewNetworkError(errs.SubtypeTLS,
			"TLS 证书校验失败: %v", err).
			WithHint("自签名证书的私有部署可加 --insecure 跳过校验").Wrap(err)
	case strings.Contains(msg, "connection refused"):
		return errs.NewNetworkError(errs.SubtypeConnRefused,
			"无法连接 %s（连接被拒绝）", c.settings.BaseURL).
			WithHint("确认站点已启动且 --base-url 正确").Wrap(err)
	case strings.Contains(msg, "no such host"):
		return errs.NewNetworkError(errs.SubtypeConnRefused,
			"域名解析失败: %s", c.settings.BaseURL).
			WithHint("检查 --base-url 拼写与 DNS").Wrap(err)
	}
	return errs.NewNetworkError("", "请求失败: %v", err).Wrap(err)
}

func retriableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

func retriableNetErr(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return strings.Contains(err.Error(), "connection reset")
}

func backoff(attempt int) time.Duration {
	return time.Duration(attempt*attempt) * 300 * time.Millisecond
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return strings.ReplaceAll(s, "\n", " ")
}
