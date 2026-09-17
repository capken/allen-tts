package provider

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/capken/allen-tts/internal/core"
)

// ErrNotImplemented 用于 v1 未覆盖的能力（如 Fish 的 voices list）。
var ErrNotImplemented = errors.New("not implemented")

// ModelInfo 是静态模型表条目。
type ModelInfo struct {
	ID      string `json:"id"`
	Default bool   `json:"default"`
	Notes   string `json:"notes,omitempty"`
}

// Range 是数值参数的合法区间；nil 表示不支持该参数。
type Range struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// Capabilities 是 adapter 的静态能力声明（设计文档 3.2）。
type Capabilities struct {
	Models       []ModelInfo      `json:"models"`
	DefaultModel string           `json:"default_model"`
	Formats      []string         `json:"formats"`
	SampleRates  map[string][]int `json:"sample_rates"`
	Bitrates     []int            `json:"bitrates,omitempty"` // kbps
	Speed        *Range           `json:"speed,omitempty"`
	Volume       *Range           `json:"volume,omitempty"` // 统一倍数量纲
	Pitch        *Range           `json:"pitch,omitempty"`
	Emotions     []string         `json:"emotions,omitempty"`
	Language     bool             `json:"language"`
	Channels     []int            `json:"channels"`
	Latency      bool             `json:"latency"`
	Stream       bool             `json:"stream"`
	MultiSpeaker bool             `json:"multi_speaker"`
}

// Usage 是计费用量。
type Usage struct {
	Characters int `json:"characters,omitempty"`
}

// Result 是一次合成的产物：音频流 + 元数据。
// Audio 已解码为原始音频字节（MiniMax hex 解码后给出）。
type Result struct {
	Audio      io.ReadCloser
	Model      string
	Format     string
	SampleRate int
	Bitrate    int // kbps，未知则 0
	Channels   int
	DurationMS int64 // 未知则 0
	Usage      Usage
	RequestID  string
	Warnings   []string
	Extras     map[string]any

	// Wait 非 nil 时（流式），在 Audio 读到 EOF 后调用，
	// 等待尾事件解析完成，之后元数据字段才完整。
	Wait func()
}

// RequestSpec 是构建好的厂商 HTTP 请求描述；--dry-run 直接序列化它。
type RequestSpec struct {
	Method  string
	URL     string
	Headers http.Header
	Body    map[string]any // JSON payload（发送前 marshal）
}

// Voice 是 voices list 的条目。
type Voice struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Language    string `json:"language,omitempty"`
}

// Provider 是厂商 adapter 接口。
// Build 负责统一参数 → 厂商 payload 的全部映射：单位换算、范围裁剪
// （写入 warnings）、不支持字段的 drop；Speak 在 Build 之上执行请求并解析响应。
type Provider interface {
	Name() string
	Capabilities() Capabilities
	Build(req core.SpeakRequest) (*RequestSpec, []string, error)
	Speak(ctx context.Context, req core.SpeakRequest) (*Result, error)
	ListVoices(ctx context.Context) ([]Voice, error)
	ListModels() []ModelInfo
}

// Checker 由支持密钥校验的 adapter 额外实现（config check 用）。
type Checker interface {
	Check(ctx context.Context) error
}
