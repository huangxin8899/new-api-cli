// Package errs 定义 new-api-cli 的类型化错误。
//
// 每个错误都携带一个 Problem，它同时是 JSON 错误信封的线上结构（`error` 字段），
// 也是退出码的来源。命令层只需返回类型化错误，root 的分发器负责渲染与退出。
package errs

import (
	"errors"
	"fmt"
	"strings"
)

// Type 是错误的一级分类，决定退出码与用户看到的语气。
type Type string

const (
	// TypeConfig 配置缺失或损坏（未执行 config init 等）。
	TypeConfig Type = "config"
	// TypeAuth 未登录、令牌过期或权限不足。
	TypeAuth Type = "auth"
	// TypeValidation 参数校验失败（本地拦截，未发出请求）。
	TypeValidation Type = "validation"
	// TypeAPI New API 服务端返回 success:false 或非 2xx。
	TypeAPI Type = "api"
	// TypeNetwork 连接失败、超时、DNS 等传输层问题。
	TypeNetwork Type = "network"
	// TypeInternal CLI 自身的缺陷。
	TypeInternal Type = "internal"
)

// 子类型：在 Type 之下细分，便于 Agent 做分支处理。
const (
	SubtypeNotConfigured   = "not_configured"
	SubtypeProfileNotFound = "profile_not_found"
	SubtypeConfigCorrupt   = "config_corrupt"

	SubtypeNotLoggedIn     = "not_logged_in"
	SubtypeTokenInvalid    = "token_invalid"
	SubtypeTokenExpired    = "token_expired"
	SubtypeForbidden       = "forbidden"
	SubtypeNeedAdmin       = "need_admin"
	SubtypeNeedRoot        = "need_root"
	Subtype2FARequired     = "2fa_required"
	SubtypeLoginFailed     = "login_failed"
	SubtypeCredentialStore = "credential_store"

	SubtypeInvalidArgument = "invalid_argument"
	SubtypeMissingArgument = "missing_argument"
	SubtypeInvalidJSON     = "invalid_json"
	SubtypeConfirmRequired = "confirm_required"
	SubtypeFileIO          = "file_io"

	SubtypeNotFound     = "not_found"
	SubtypeRateLimited  = "rate_limited"
	SubtypeServerError  = "server_error"
	SubtypeBadResponse  = "bad_response"
	SubtypeUnauthorized = "unauthorized"

	SubtypeTimeout     = "timeout"
	SubtypeConnRefused = "connection_refused"
	SubtypeTLS         = "tls"
)

// Problem 是错误信封中 `error` 字段的线上结构。
type Problem struct {
	Type       Type     `json:"type"`
	Subtype    string   `json:"subtype,omitempty"`
	Code       int      `json:"code,omitempty"`
	HTTPStatus int      `json:"http_status,omitempty"`
	Message    string   `json:"message"`
	Hint       string   `json:"hint,omitempty"`
	Params     []string `json:"params,omitempty"`
	RequestID  string   `json:"request_id,omitempty"`
}

// Error 是所有类型化错误的统一实现。
type Error struct {
	Problem
	wrapped error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if e.Hint != "" {
		b.WriteString("\n提示: ")
		b.WriteString(e.Hint)
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.wrapped }

// WithHint 附加一条可执行的修复建议。
func (e *Error) WithHint(format string, a ...any) *Error {
	e.Hint = fmt.Sprintf(format, a...)
	return e
}

// WithParams 记录涉及的参数名，便于 Agent 定位改哪个 flag。
func (e *Error) WithParams(params ...string) *Error {
	e.Params = append(e.Params, params...)
	return e
}

// WithCode 记录 New API 返回的业务错误码。
func (e *Error) WithCode(code int) *Error {
	e.Code = code
	return e
}

// WithRequestID 记录服务端请求 ID，便于对账排查。
func (e *Error) WithRequestID(id string) *Error {
	e.RequestID = id
	return e
}

// Wrap 保留底层错误以支持 errors.Is / errors.As。
func (e *Error) Wrap(err error) *Error {
	e.wrapped = err
	return e
}

func newError(t Type, subtype, format string, a ...any) *Error {
	return &Error{Problem: Problem{Type: t, Subtype: subtype, Message: fmt.Sprintf(format, a...)}}
}

// NewConfigError 构造配置类错误。
func NewConfigError(subtype, format string, a ...any) *Error {
	return newError(TypeConfig, subtype, format, a...)
}

// NewAuthError 构造认证/授权类错误。
func NewAuthError(subtype, format string, a ...any) *Error {
	return newError(TypeAuth, subtype, format, a...)
}

// NewValidationError 构造本地参数校验错误。
func NewValidationError(subtype, format string, a ...any) *Error {
	return newError(TypeValidation, subtype, format, a...)
}

// NewAPIError 构造服务端返回的错误。
func NewAPIError(subtype, format string, a ...any) *Error {
	return newError(TypeAPI, subtype, format, a...)
}

// NewNetworkError 构造传输层错误。
func NewNetworkError(subtype, format string, a ...any) *Error {
	return newError(TypeNetwork, subtype, format, a...)
}

// NewInternalError 构造 CLI 自身缺陷导致的错误。
func NewInternalError(format string, a ...any) *Error {
	return newError(TypeInternal, "", format, a...)
}

// Unwrap 从错误链中取出类型化错误。
func Unwrap(err error) (*Error, bool) {
	var typed *Error
	if errors.As(err, &typed) {
		return typed, true
	}
	return nil, false
}

// 退出码契约。数值一旦发布即为公共接口，Agent 会据此分支。
const (
	ExitOK         = 0
	ExitError      = 1 // 通用/内部错误
	ExitUsage      = 2 // 用法错误（cobra 参数解析失败）
	ExitConfig     = 3 // 未配置或配置损坏
	ExitAuth       = 4 // 未登录、令牌失效
	ExitForbidden  = 5 // 已登录但权限不足
	ExitValidation = 6 // 本地参数校验失败
	ExitAPI        = 7 // 服务端返回错误
	ExitNetwork    = 8 // 网络不可达
	ExitNotFound   = 9 // 资源不存在
)

// ExitCodeOf 把错误映射为进程退出码。
func ExitCodeOf(err error) int {
	if err == nil {
		return ExitOK
	}
	typed, ok := Unwrap(err)
	if !ok {
		return ExitError
	}
	switch typed.Type {
	case TypeConfig:
		return ExitConfig
	case TypeAuth:
		switch typed.Subtype {
		case SubtypeForbidden, SubtypeNeedAdmin, SubtypeNeedRoot:
			return ExitForbidden
		default:
			return ExitAuth
		}
	case TypeValidation:
		return ExitValidation
	case TypeAPI:
		if typed.Subtype == SubtypeNotFound {
			return ExitNotFound
		}
		return ExitAPI
	case TypeNetwork:
		return ExitNetwork
	default:
		return ExitError
	}
}
