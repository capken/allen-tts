// Package httpx 提供三家 adapter 共享的 HTTP 执行与脱敏工具。
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/capken/allen-tts/internal/core"
	"github.com/capken/allen-tts/internal/version"
)

// UserAgent 是所有出站请求必带的 UA（设计文档第 7 节）。
func UserAgent() string {
	return "allen-tts/" + version.Version
}

// NewClient 返回带超时的共享 client。
func NewClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// Spec 与 provider.RequestSpec 字段一致；为避免包环依赖在此重复定义最小集。
type Spec struct {
	Method  string
	URL     string
	Headers http.Header
	Body    map[string]any
}

// Do 序列化 body、附加 UA 后执行请求。网络层错误统一映射为 core.ErrNetwork。
func Do(ctx context.Context, client *http.Client, provider string, spec Spec) (*http.Response, error) {
	var body *bytes.Reader
	if spec.Body != nil {
		raw, err := json.Marshal(spec.Body)
		if err != nil {
			return nil, core.NewError(core.ErrInvalidArgument, "cannot encode payload: %v", err).WithProvider(provider)
		}
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, spec.Method, spec.URL, body)
	if err != nil {
		return nil, core.NewError(core.ErrInvalidArgument, "bad request: %v", err).WithProvider(provider)
	}
	for k, vs := range spec.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("User-Agent", UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		code := core.ErrNetwork
		if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
			return nil, core.NewError(code, "request timed out: %v", err).WithProvider(provider)
		}
		return nil, core.NewError(code, "network error: %v", err).WithProvider(provider)
	}
	return resp, nil
}

// RedactAuth 返回打码后的 Authorization 值：保留 Bearer 前缀与 token 前 6 位。
func RedactAuth(v string) string {
	const prefix = "Bearer "
	token := v
	hasBearer := len(v) > len(prefix) && v[:len(prefix)] == prefix
	if hasBearer {
		token = v[len(prefix):]
	}
	if len(token) > 6 {
		token = token[:6] + "****"
	} else if token != "" {
		token = "****"
	}
	if hasBearer {
		return prefix + token
	}
	return token
}

// RedactHeaders 输出可打印的 header 副本，Authorization 打码。
func RedactHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k := range h {
		v := h.Get(k)
		if http.CanonicalHeaderKey(k) == "Authorization" {
			v = RedactAuth(v)
		}
		out[k] = v
	}
	return out
}

// ReadErrBody 读取错误响应体的前 512 字节用于 message。
func ReadErrBody(resp *http.Response) string {
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	resp.Body.Close()
	return string(bytes.TrimSpace(buf[:n]))
}

// StatusMsg 拼装带状态码的错误消息。
func StatusMsg(resp *http.Response, detail string) string {
	if detail == "" {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, detail)
}
