package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api-cli/errs"
	"github.com/QuantumNous/new-api-cli/internal/config"
)

// newTestClient 指向一个测试服务器，省去配置文件依赖。
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	return New(&config.Settings{
		BaseURL:     baseURL,
		Token:       "sk-test-token",
		Timeout:     5,
		Profile:     "test",
		TokenSource: "--token",
	})
}

func TestNormalizePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/api/channel/", "/api/channel/"},
		{"channel/", "/api/channel/"},
		{"/channel/", "/api/channel/"},
		{"/api/status", "/api/status"},
		{"status", "/api/status"},
		// relay 侧路径必须原样保留，不能被塞上 /api。
		{"/v1/models", "/v1/models"},
		{"/mj/submit", "/mj/submit"},
		{"/pg/x", "/pg/x"},
		// 完整 URL 只取路径部分。
		{"https://example.com/api/channel/", "/api/channel/"},
		{"https://example.com/api/log/?p=2", "/api/log/?p=2"},
	}
	for _, c := range cases {
		if got := NormalizePath(c.in); got != c.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// New API 的业务失败也返回 HTTP 200，这是最容易被误判成成功的地方。
func TestDo_BusinessFailureOnHTTP200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":false,"message":"该渠道不存在"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL).Do(context.Background(), Request{Method: "GET", Path: "/api/channel/999"})
	if err == nil {
		t.Fatal("HTTP 200 + success:false 必须视为错误")
	}
	typed, ok := errs.Unwrap(err)
	if !ok {
		t.Fatalf("期望类型化错误，得到 %T", err)
	}
	if typed.Type != errs.TypeAPI {
		t.Errorf("Type = %q, want %q", typed.Type, errs.TypeAPI)
	}
	// "不存在" 应映射到 not_found，从而给出退出码 9。
	if typed.Subtype != errs.SubtypeNotFound {
		t.Errorf("Subtype = %q, want %q", typed.Subtype, errs.SubtypeNotFound)
	}
	if got := errs.ExitCodeOf(err); got != errs.ExitNotFound {
		t.Errorf("exit code = %d, want %d", got, errs.ExitNotFound)
	}
}

func TestDo_SuccessEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"id":7,"name":"openai-main"}}`))
	}))
	defer srv.Close()

	resp, err := newTestClient(t, srv.URL).Do(context.Background(), Request{Method: "GET", Path: "/api/channel/7"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("Success = false, want true")
	}
	var got map[string]any
	if err := resp.Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got["name"] != "openai-main" {
		t.Errorf("data.name = %v, want openai-main", got["name"])
	}
}

func TestDo_SendsAuthAndOriginHeaders(t *testing.T) {
	var gotAuth, gotOrigin, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotOrigin = r.Header.Get("Origin")
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"success":true,"data":null}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL).Do(context.Background(), Request{Method: "GET", Path: "/api/status"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer sk-test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	// New API 对 cookie 认证接口做同源校验，Origin 必须等于站点地址。
	if gotOrigin != srv.URL {
		t.Errorf("Origin = %q, want %q", gotOrigin, srv.URL)
	}
	if !strings.HasPrefix(gotUA, "new-api-cli/") {
		t.Errorf("User-Agent = %q, want new-api-cli/ 前缀", gotUA)
	}
}

func TestDo_NoAuthSkipsAuthorization(t *testing.T) {
	var hasAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"success":true,"data":null}`))
	}))
	defer srv.Close()

	c := New(&config.Settings{BaseURL: srv.URL, Timeout: 5, Profile: "test"})
	// 无令牌 + NoAuth 也应放行，否则 config init 的探活会失败。
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/api/status", NoAuth: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasAuth {
		t.Error("NoAuth 请求不应带 Authorization 头")
	}
}

func TestDo_RequiresTokenWhenAuthed(t *testing.T) {
	c := New(&config.Settings{BaseURL: "https://example.com", Timeout: 5, Profile: "default"})
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/api/channel/"})
	if err == nil {
		t.Fatal("缺少令牌时应直接报错，不发请求")
	}
	if got := errs.ExitCodeOf(err); got != errs.ExitAuth {
		t.Errorf("exit code = %d, want %d", got, errs.ExitAuth)
	}
}

func TestDo_HTTPStatusMapping(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		wantExit int
		wantType errs.Type
	}{
		{http.StatusUnauthorized, `{"success":false,"message":"无效的令牌"}`, errs.ExitAuth, errs.TypeAuth},
		{http.StatusForbidden, `{"success":false,"message":"权限不足"}`, errs.ExitForbidden, errs.TypeAuth},
		{http.StatusNotFound, `{"success":false,"message":"not found"}`, errs.ExitNotFound, errs.TypeAPI},
		{http.StatusInternalServerError, `{"success":false,"message":"boom"}`, errs.ExitAPI, errs.TypeAPI},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		_, err := newTestClient(t, srv.URL).Do(context.Background(), Request{Method: "GET", Path: "/api/x"})
		srv.Close()

		if err == nil {
			t.Errorf("HTTP %d 应产生错误", c.status)
			continue
		}
		if got := errs.ExitCodeOf(err); got != c.wantExit {
			t.Errorf("HTTP %d: exit = %d, want %d", c.status, got, c.wantExit)
		}
		if typed, ok := errs.Unwrap(err); ok && typed.Type != c.wantType {
			t.Errorf("HTTP %d: type = %q, want %q", c.status, typed.Type, c.wantType)
		}
	}
}

// 指向非 New API 服务时要给出可诊断的错误，而不是 JSON 解析噪音。
func TestDo_NonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>nginx welcome</body></html>"))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL).Do(context.Background(), Request{Method: "GET", Path: "/api/status"})
	if err == nil {
		t.Fatal("非 JSON 响应应报错")
	}
	typed, _ := errs.Unwrap(err)
	if typed.Subtype != errs.SubtypeBadResponse {
		t.Errorf("Subtype = %q, want %q", typed.Subtype, errs.SubtypeBadResponse)
	}
	if !strings.Contains(typed.Hint, "base-url") {
		t.Errorf("提示应指向 --base-url，得到 %q", typed.Hint)
	}
}

func TestDo_POSTNotRetried(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"success":false,"message":"unavailable"}`))
	}))
	defer srv.Close()

	// POST 重试会重复创建资源，必须只发一次。
	_, _ = newTestClient(t, srv.URL).Do(context.Background(), Request{
		Method: "POST", Path: "/api/channel/", Body: map[string]any{"name": "x"},
	})
	if calls != 1 {
		t.Errorf("POST 请求次数 = %d, want 1（不得自动重试）", calls)
	}
}

func TestDo_GETRetriesOn503(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"ok":1}}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL).Do(context.Background(), Request{Method: "GET", Path: "/api/status"}); err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if calls != 3 {
		t.Errorf("GET 请求次数 = %d, want 3", calls)
	}
}

func TestList_PaginatesAll(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("p")
		pages = append(pages, p)
		if r.URL.Query().Get("page_size") != "2" {
			t.Errorf("page_size = %q, want 2", r.URL.Query().Get("page_size"))
		}
		var items string
		switch p {
		case "1":
			items = `[{"id":1},{"id":2}]`
		case "2":
			items = `[{"id":3},{"id":4}]`
		default:
			items = `[{"id":5}]`
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"page":` + p + `,"page_size":2,"total":5,"items":` + items + `}}`))
	}))
	defer srv.Close()

	res, err := newTestClient(t, srv.URL).List(context.Background(),
		Request{Method: "GET", Path: "/api/channel/"},
		ListOptions{All: true, PageSize: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Items) != 5 {
		t.Errorf("取回 %d 条, want 5", len(res.Items))
	}
	if res.Total != 5 {
		t.Errorf("Total = %d, want 5", res.Total)
	}
	if strings.Join(pages, ",") != "1,2,3" {
		t.Errorf("翻页序列 = %v, want [1 2 3]", pages)
	}
}

func TestList_LimitTruncates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"page":1,"page_size":2,"total":100,"items":[{"id":1},{"id":2}]}}`))
	}))
	defer srv.Close()

	res, err := newTestClient(t, srv.URL).List(context.Background(),
		Request{Method: "GET", Path: "/api/channel/"},
		ListOptions{All: true, PageSize: 2, Limit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Items) != 3 {
		t.Errorf("取回 %d 条, want 3（受 --limit 限制）", len(res.Items))
	}
	if !res.Truncated {
		t.Error("Truncated 应为 true，以便提示用户还有更多数据")
	}
}

// 部分接口（如 /api/group/）直接返回数组而非分页对象。
func TestList_BareArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":["default","vip"]}`))
	}))
	defer srv.Close()

	res, err := newTestClient(t, srv.URL).List(context.Background(),
		Request{Method: "GET", Path: "/api/group/"}, ListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("取回 %d 条, want 2", len(res.Items))
	}
	if res.Items[0] != "default" {
		t.Errorf("Items[0] = %v, want default", res.Items[0])
	}
}

func TestDo_QueryParamsPreserved(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"success":true,"data":null}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL).Do(context.Background(), Request{
		Method: "GET",
		Path:   "/api/log/",
		Query:  map[string][]string{"type": {"2"}, "username": {"alice"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "type=2") || !strings.Contains(got, "username=alice") {
		t.Errorf("RawQuery = %q, 应含 type=2 与 username=alice", got)
	}
}

func TestDo_NetworkErrorIsTyped(t *testing.T) {
	// 立即关闭的服务器 → 连接被拒绝。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := newTestClient(t, url).Do(context.Background(), Request{Method: "POST", Path: "/api/x"})
	if err == nil {
		t.Fatal("连接失败应报错")
	}
	if got := errs.ExitCodeOf(err); got != errs.ExitNetwork {
		t.Errorf("exit = %d, want %d", got, errs.ExitNetwork)
	}
}

func TestResponse_DecodeNullData(t *testing.T) {
	r := &Response{Data: json.RawMessage("null")}
	var m map[string]any
	if err := r.Decode(&m); err != nil {
		t.Errorf("data:null 不应报错: %v", err)
	}
	if m != nil {
		t.Errorf("m = %v, want nil", m)
	}
}
