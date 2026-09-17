// Package cartesia 实现 Cartesia /tts/bytes adapter。
// 特点：必填版本头、嵌套 output_format、generation_config 仅 sonic-3+（设计文档 7.2）。
package cartesia

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/capken/allen-tts/internal/core"
	"github.com/capken/allen-tts/internal/provider"
	"github.com/capken/allen-tts/internal/provider/httpx"
)

const defaultBaseURL = "https://api.cartesia.ai"
const defaultModel = "sonic-3.6"
const apiVersion = "2026-08-14"

type Cartesia struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

type Options struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

func New(o Options) *Cartesia {
	base := o.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return &Cartesia{apiKey: o.APIKey, baseURL: strings.TrimRight(base, "/"), model: o.Model, client: httpx.NewClient(o.Timeout)}
}

func (c *Cartesia) Name() string { return "cartesia" }

func (c *Cartesia) ListModels() []provider.ModelInfo {
	return []provider.ModelInfo{
		{ID: "sonic-3.6", Default: true, Notes: "最新旗舰"},
		{ID: "sonic-3.5"},
		{ID: "sonic-3"},
		{ID: "sonic-latest", Notes: "自动指向最新 sonic"},
	}
}

var allSampleRates = []int{8000, 16000, 22050, 24000, 44100, 48000}

func (c *Cartesia) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Models:       c.ListModels(),
		DefaultModel: defaultModel,
		Formats:      []string{"mp3", "wav", "pcm"},
		SampleRates: map[string][]int{
			"mp3": allSampleRates, "wav": allSampleRates, "pcm": allSampleRates,
		},
		Bitrates: []int{32, 64, 96, 128, 192},
		Speed:    &provider.Range{Min: 0.6, Max: 1.5},
		Volume:   &provider.Range{Min: 0.5, Max: 2.0},
		Emotions: []string{"neutral", "happy", "sad", "angry", "scared", "surprised", "calm", "excited"},
		Language: true,
		Channels: []int{1},
		Stream:   false,
	}
}

// emotionMap 把统一情绪枚举映射到 Cartesia 枚举；未列出的值原样透传。
var emotionMap = map[string]string{
	"neutral": "neutral", "happy": "happy", "sad": "sad", "angry": "angry",
	"fearful": "scared", "surprised": "surprised", "calm": "calm", "excited": "excited",
}

// supportsGenerationConfig：generation_config 仅 sonic-3 及以上支持。
// sonic-latest 按 ≥ sonic-3 处理（设计文档开放问题 1）。
func supportsGenerationConfig(model string) bool {
	return strings.HasPrefix(model, "sonic-3") || model == "sonic-latest"
}

func (c *Cartesia) Build(req core.SpeakRequest) (*provider.RequestSpec, []string, error) {
	n := core.NewNorm(req.OnUnsupported)

	model := req.Model
	if model == "" {
		model = c.model
	}
	if model == "" {
		model = defaultModel
	}

	payload := map[string]any{
		"model_id":   model,
		"transcript": req.Text,
	}
	if req.Voice != "" {
		payload["voice"] = req.Voice // 直传字符串，不包成对象
	}
	if req.Language != "" {
		payload["language"] = req.Language // 永远不写 locale
	}

	gen := map[string]any{}
	if req.Speed != 1.0 {
		gen["speed"] = n.ClampF("speed", req.Speed, 0.6, 1.5)
	}
	if req.Volume != 1.0 {
		gen["volume"] = n.ClampF("volume", req.Volume, 0.5, 2.0)
	}
	if req.Emotion != "" {
		if mapped, ok := emotionMap[req.Emotion]; ok {
			gen["emotion"] = mapped
		} else {
			gen["emotion"] = req.Emotion // 不在统一枚举内 → 原样透传
		}
	}
	if len(gen) > 0 {
		if supportsGenerationConfig(model) {
			payload["generation_config"] = gen
		} else {
			n.Drop("speed/volume/emotion", "generation_config requires sonic-3 or later, model is "+model)
		}
	}
	if req.Pitch != 0 {
		n.Drop("pitch", "cartesia does not support pitch")
	}

	sr := req.SampleRate
	if sr == 0 {
		sr = 44100
	}
	sr = n.Nearest("sample_rate", sr, allSampleRates)

	switch req.Format {
	case "mp3":
		br := req.Bitrate * 1000
		if br == 0 {
			br = 128000
		}
		br = n.Nearest("bitrate", br, []int{32000, 64000, 96000, 128000, 192000})
		payload["output_format"] = map[string]any{"container": "mp3", "sample_rate": sr, "bit_rate": br}
	case "wav":
		payload["output_format"] = map[string]any{"container": "wav", "encoding": "pcm_s16le", "sample_rate": sr}
	case "pcm":
		payload["output_format"] = map[string]any{"container": "raw", "encoding": "pcm_s16le", "sample_rate": sr}
	default:
		// 格式不支持永远报错（设计文档 4.3）。
		return nil, nil, core.NewError(core.ErrInvalidArgument,
			"cartesia does not support format %q (supported: mp3, wav, pcm)", req.Format).WithProvider("cartesia")
	}

	if req.Channels == 2 {
		n.Drop("channels", "cartesia only supports mono output")
	}
	// Latency drop 且不 warn：Cartesia 本身低延迟。
	if req.Stream {
		n.Warnings = append(n.Warnings, "cartesia streaming not supported in v1, falling back to non-streaming")
	}

	if req.Extra != nil {
		payload = core.DeepMerge(payload, req.Extra)
	}
	if err := n.Err(); err != nil {
		return nil, nil, err
	}

	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.apiKey)
	h.Set("Cartesia-Version", apiVersion)
	h.Set("Content-Type", "application/json")

	return &provider.RequestSpec{
		Method:  http.MethodPost,
		URL:     c.baseURL + "/tts/bytes",
		Headers: h,
		Body:    payload,
	}, n.Warnings, nil
}

func (c *Cartesia) Speak(ctx context.Context, req core.SpeakRequest) (*provider.Result, error) {
	spec, warnings, err := c.Build(req)
	if err != nil {
		return nil, err
	}
	resp, err := httpx.Do(ctx, c.client, "cartesia", httpx.Spec{Method: spec.Method, URL: spec.URL, Headers: spec.Headers, Body: spec.Body})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		detail := httpx.ReadErrBody(resp)
		var e *core.Error
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			e = core.NewError(core.ErrAuth, "%s", httpx.StatusMsg(resp, detail))
		case resp.StatusCode == http.StatusPaymentRequired:
			e = core.NewError(core.ErrQuota, "%s", httpx.StatusMsg(resp, detail))
		case resp.StatusCode == http.StatusTooManyRequests:
			e = core.NewError(core.ErrRateLimit, "%s", httpx.StatusMsg(resp, detail))
		case resp.StatusCode >= 500:
			e = core.NewError(core.ErrProvider, "%s", httpx.StatusMsg(resp, detail))
		default:
			e = core.NewError(core.ErrInvalidArgument, "%s", httpx.StatusMsg(resp, detail))
		}
		return nil, e.WithProvider("cartesia").WithHTTP(resp.StatusCode)
	}
	model, _ := spec.Body["model_id"].(string)
	sr := 0
	if of, ok := spec.Body["output_format"].(map[string]any); ok {
		if v, ok := of["sample_rate"].(int); ok {
			sr = v
		}
	}
	return &provider.Result{
		Audio:      resp.Body,
		Model:      model,
		Format:     req.Format,
		SampleRate: sr,
		Bitrate:    req.Bitrate,
		Channels:   1,
		Warnings:   warnings,
	}, nil
}

// ListVoices 调 GET /voices/，单页最多 100 条。
func (c *Cartesia) ListVoices(ctx context.Context) ([]provider.Voice, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.apiKey)
	h.Set("Cartesia-Version", apiVersion)
	resp, err := httpx.Do(ctx, c.client, "cartesia", httpx.Spec{Method: http.MethodGet, URL: c.baseURL + "/voices/?limit=100", Headers: h})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, core.NewError(core.ErrAuth, "cartesia API key rejected (HTTP %d)", resp.StatusCode).WithProvider("cartesia").WithHTTP(resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, core.NewError(core.ErrProvider, "unexpected HTTP %d from cartesia", resp.StatusCode).WithProvider("cartesia").WithHTTP(resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Language    string `json:"language"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, core.NewError(core.ErrProvider, "cannot parse voices response: %v", err).WithProvider("cartesia")
	}
	out := make([]provider.Voice, 0, len(body.Data))
	for _, v := range body.Data {
		out = append(out, provider.Voice{ID: v.ID, Name: v.Name, Description: v.Description, Language: v.Language})
	}
	return out, nil
}

// Check 用 voices 列表接口校验密钥。
func (c *Cartesia) Check(ctx context.Context) error {
	_, err := c.ListVoices(ctx)
	return err
}

var _ provider.Provider = (*Cartesia)(nil)
