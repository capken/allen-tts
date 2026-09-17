package core

import "fmt"

// ErrCode 是统一错误码，跨厂商稳定，agent 可据此决定重试或切换厂商。
type ErrCode string

const (
	ErrInvalidArgument ErrCode = "invalid_argument"
	ErrAuth            ErrCode = "auth"
	ErrRateLimit       ErrCode = "rate_limit"
	ErrQuota           ErrCode = "quota"
	ErrInvalidText     ErrCode = "invalid_text"
	ErrProvider        ErrCode = "provider_error"
	ErrNetwork         ErrCode = "network"
)

// Error 是贯穿全程的统一错误类型，可直接序列化进 --json 失败输出。
type Error struct {
	Code         ErrCode `json:"code"`
	Provider     string  `json:"provider,omitempty"`
	ProviderCode string  `json:"provider_code,omitempty"`
	HTTPStatus   int     `json:"http_status,omitempty"`
	Message      string  `json:"message"`
	Retryable    bool    `json:"retryable"`
}

func (e *Error) Error() string {
	if e.Provider != "" {
		return fmt.Sprintf("[%s] %s: %s", e.Provider, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ExitCode 按设计文档第 6.4 节映射退出码。
func (e *Error) ExitCode() int {
	switch e.Code {
	case ErrInvalidArgument:
		return 2
	case ErrAuth:
		return 3
	case ErrRateLimit, ErrQuota:
		return 4
	case ErrInvalidText:
		return 5
	case ErrProvider:
		return 6
	case ErrNetwork:
		return 7
	default:
		return 6
	}
}

func retryable(code ErrCode) bool {
	switch code {
	case ErrRateLimit, ErrQuota, ErrProvider, ErrNetwork:
		return true
	}
	return false
}

// NewError 创建统一错误并按错误码填充 Retryable。
func NewError(code ErrCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Retryable: retryable(code)}
}

// WithProvider 补充厂商上下文，返回自身便于链式调用。
func (e *Error) WithProvider(name string) *Error {
	e.Provider = name
	return e
}

// WithHTTP 补充 HTTP 状态码。
func (e *Error) WithHTTP(status int) *Error {
	e.HTTPStatus = status
	return e
}

// WithProviderCode 补充厂商原始错误码（如 MiniMax base_resp.status_code）。
func (e *Error) WithProviderCode(code string) *Error {
	e.ProviderCode = code
	return e
}
