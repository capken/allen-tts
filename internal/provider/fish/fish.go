// Package fish 实现 Fish Audio /v1/tts adapter。
// 特点：模型走 HTTP header、扁平 body、音量量纲为 dB（设计文档 7.1）。
package fish

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/capken/allen-tts/internal/core"
	"github.com/capken/allen-tts/internal/provider"
	"github.com/capken/allen-tts/internal/provider/httpx"
)

const defaultBaseURL = "https://api.fish.audio"
const defaultModel = "s2.1-pro"

type Fish struct {
	apiKey  string
	baseURL string
	model   string // 配置层默认模型
	client  *http.Client
}

type Options struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

func New(o Options) *Fish {
	base := o.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return &Fish{apiKey: o.APIKey, baseURL: strings.TrimRight(base, "/"), model: o.Model, client: httpx.NewClient(o.Timeout)}
}

func (f *Fish) Name() string { return "fish" }

func (f *Fish) ListModels() []provider.ModelInfo {
	return []provider.ModelInfo{
		{ID: "s2.1-pro", Default: true, Notes: "最新旗舰"},
		{ID: "s2.1-pro-free", Notes: "免费档"},
		{ID: "s2-pro"},
		{ID: "s1"},
		{ID: "drama-3-preview", Notes: "戏剧化表现，preview"},
	}
}

var sampleRates = map[string][]int{
	"wav":  {8000, 16000, 24000, 32000, 44100},
	"pcm":  {8000, 16000, 24000, 32000, 44100},
	"mp3":  {32000, 44100},
	"opus": {48000},
}

func (f *Fish) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Models:       f.ListModels(),
		DefaultModel: defaultModel,
		Formats:      []string{"mp3", "wav", "pcm", "opus"},
		SampleRates:  sampleRates,
		Bitrates:     []int{64, 128, 192},
		Speed:        &provider.Range{Min: 0.5, Max: 2.0},
		Volume:       &provider.Range{Min: 0.5, Max: 2.0},
		Channels:     []int{1},
		Latency:      true,
		Stream:       true,
		MultiSpeaker: true,
	}
}

// volumeToDB 把统一倍数换算为 Fish 的 dB：20*log10(v)，保留 1 位小数。
func volumeToDB(v float64) float64 {
	return math.Round(20*math.Log10(v)*10) / 10
}

func (f *Fish) Build(req core.SpeakRequest) (*provider.RequestSpec, []string, error) {
	n := core.NewNorm(req.OnUnsupported)

	model := req.Model
	if model == "" {
		model = f.model
	}
	if model == "" {
		model = defaultModel
	}

	payload := map[string]any{"text": req.Text}

	if req.Voice != "" {
		if strings.Contains(req.Voice, ",") {
			parts := strings.Split(req.Voice, ",")
			ids := make([]any, 0, len(parts))
			for _, p := range parts {
				ids = append(ids, strings.TrimSpace(p))
			}
			payload["reference_id"] = ids
		} else {
			payload["reference_id"] = req.Voice
		}
	}
	if req.Language != "" {
		n.Drop("language", "fish does not support language selection")
	}

	speed := n.ClampF("speed", req.Speed, 0.5, 2.0)
	vol := n.ClampF("volume", req.Volume, 0.5, 2.0)
	if speed != 1.0 || vol != 1.0 {
		payload["prosody"] = map[string]any{"speed": speed, "volume": volumeToDB(vol)}
	}
	if req.Pitch != 0 {
		n.Drop("pitch", "fish does not support pitch")
	}
	if req.Emotion != "" {
		n.Drop("emotion", "fish does not support emotion (use --extra temperature/top_p)")
	}

	// 格式不支持永远报错（设计文档 4.3），不受 on-unsupported 影响。
	switch req.Format {
	case "mp3", "wav", "pcm", "opus":
		payload["format"] = req.Format
	default:
		return nil, nil, core.NewError(core.ErrInvalidArgument,
			"fish does not support format %q (supported: mp3, wav, pcm, opus)", req.Format).WithProvider("fish")
	}
	if req.SampleRate > 0 {
		payload["sample_rate"] = n.Nearest("sample_rate", req.SampleRate, sampleRates[req.Format])
	}
	if req.Bitrate > 0 {
		switch req.Format {
		case "mp3":
			payload["mp3_bitrate"] = n.Nearest("bitrate", req.Bitrate, []int{64, 128, 192})
		case "opus":
			payload["opus_bitrate"] = n.Nearest("bitrate", req.Bitrate*1000, []int{24000, 32000, 48000, 64000})
		}
	}
	if req.Channels == 2 {
		n.Drop("channels", "fish only supports mono output")
	}
	switch req.Latency {
	case "balanced":
		payload["latency"] = "balanced"
	case "low":
		payload["latency"] = "low"
	default:
		payload["latency"] = "normal"
	}

	if req.Extra != nil {
		payload = core.DeepMerge(payload, req.Extra)
	}
	if err := n.Err(); err != nil {
		return nil, nil, err
	}

	h := http.Header{}
	h.Set("Authorization", "Bearer "+f.apiKey)
	h.Set("Content-Type", "application/json")
	h.Set("model", model)

	return &provider.RequestSpec{
		Method:  http.MethodPost,
		URL:     f.baseURL + "/v1/tts",
		Headers: h,
		Body:    payload,
	}, n.Warnings, nil
}

func (f *Fish) Speak(ctx context.Context, req core.SpeakRequest) (*provider.Result, error) {
	spec, warnings, err := f.Build(req)
	if err != nil {
		return nil, err
	}
	resp, err := httpx.Do(ctx, f.client, "fish", httpx.Spec{Method: spec.Method, URL: spec.URL, Headers: spec.Headers, Body: spec.Body})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		detail := httpx.ReadErrBody(resp)
		var e *core.Error
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			e = core.NewError(core.ErrAuth, "%s", httpx.StatusMsg(resp, "invalid or missing API key"))
		case resp.StatusCode == http.StatusPaymentRequired:
			e = core.NewError(core.ErrQuota, "%s", httpx.StatusMsg(resp, detail))
		case resp.StatusCode == http.StatusServiceUnavailable:
			e = core.NewError(core.ErrRateLimit, "%s", httpx.StatusMsg(resp, detail))
		case resp.StatusCode >= 500:
			e = core.NewError(core.ErrProvider, "%s", httpx.StatusMsg(resp, detail))
		default:
			e = core.NewError(core.ErrInvalidArgument, "%s", httpx.StatusMsg(resp, detail))
		}
		return nil, e.WithProvider("fish").WithHTTP(resp.StatusCode)
	}
	return &provider.Result{
		Audio:      resp.Body, // 天然 chunked，逐块读即是流式
		Model:      spec.Headers.Get("model"),
		Format:     req.Format,
		SampleRate: req.SampleRate,
		Bitrate:    req.Bitrate,
		Channels:   1,
		Warnings:   warnings,
	}, nil
}

func (f *Fish) ListVoices(ctx context.Context) ([]provider.Voice, error) {
	return nil, provider.ErrNotImplemented
}

// Check 用钱包接口做最小鉴权校验（不产生合成费用）。
func (f *Fish) Check(ctx context.Context) error {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+f.apiKey)
	resp, err := httpx.Do(ctx, f.client, "fish", httpx.Spec{Method: http.MethodGet, URL: f.baseURL + "/wallet/self/api-credit", Headers: h})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return core.NewError(core.ErrAuth, "fish API key rejected (HTTP %d)", resp.StatusCode).WithProvider("fish").WithHTTP(resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return core.NewError(core.ErrProvider, "unexpected HTTP %d from fish", resp.StatusCode).WithProvider("fish").WithHTTP(resp.StatusCode)
	}
	return nil
}

var _ provider.Provider = (*Fish)(nil)
