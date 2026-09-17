// Package minimax 实现 MiniMax /v1/t2a_v2 adapter。
// 特点：JSON+hex 响应、HTTP 200 内的 base_resp 软错误、SSE 流式（设计文档 7.3）。
package minimax

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/capken/allen-tts/internal/core"
	"github.com/capken/allen-tts/internal/provider"
	"github.com/capken/allen-tts/internal/provider/httpx"
)

const defaultBaseURL = "https://api.minimax.cn"
const defaultModel = "speech-2.8-hd"

type MiniMax struct {
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

func New(o Options) *MiniMax {
	base := o.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	return &MiniMax{apiKey: o.APIKey, baseURL: strings.TrimRight(base, "/"), model: o.Model, client: httpx.NewClient(o.Timeout)}
}

func (m *MiniMax) Name() string { return "minimax" }

func (m *MiniMax) ListModels() []provider.ModelInfo {
	return []provider.ModelInfo{
		{ID: "speech-2.8-hd", Default: true, Notes: "最新 HD"},
		{ID: "speech-2.8-turbo", Notes: "低延迟"},
		{ID: "speech-2.6-hd", Notes: "支持 fluent 情绪"},
		{ID: "speech-2.6-turbo", Notes: "支持 fluent 情绪"},
		{ID: "speech-02-hd"},
		{ID: "speech-02-turbo"},
		{ID: "speech-01-hd"},
		{ID: "speech-01-turbo"},
	}
}

var sampleRates = []int{8000, 16000, 22050, 24000, 32000, 44100}

func (m *MiniMax) Capabilities() provider.Capabilities {
	rates := map[string][]int{}
	for _, f := range []string{"mp3", "wav", "pcm", "opus", "flac"} {
		rates[f] = sampleRates
	}
	return provider.Capabilities{
		Models:       m.ListModels(),
		DefaultModel: defaultModel,
		Formats:      []string{"mp3", "wav", "pcm", "opus", "flac"},
		SampleRates:  rates,
		Bitrates:     []int{32, 64, 128, 256},
		Speed:        &provider.Range{Min: 0.5, Max: 2.0},
		Volume:       &provider.Range{Min: 0.5, Max: 2.0}, // 统一上限 2.0；更大值走 --extra
		Pitch:        &provider.Range{Min: -12, Max: 12},
		Emotions:     []string{"neutral", "happy", "sad", "angry", "fearful", "disgusted", "surprised", "calm", "excited", "fluent", "whisper"},
		Language:     true,
		Channels:     []int{1, 2},
		Latency:      true,
		Stream:       true,
	}
}

// languageBoost 把 BCP-47 映射为 MiniMax 的英文语言名（设计文档 7.3）。
func languageBoost(lang string) (string, bool) {
	l := strings.ToLower(lang)
	switch {
	case l == "zh" || l == "zh-cn":
		return "Chinese", true
	case l == "zh-hk" || l == "zh-tw" || l == "yue":
		return "Chinese,Yue", true
	case l == "en" || strings.HasPrefix(l, "en-"):
		return "English", true
	case l == "auto":
		return "auto", true
	}
	table := map[string]string{
		"ja": "Japanese", "ko": "Korean", "fr": "French", "de": "German",
		"es": "Spanish", "pt": "Portuguese", "ru": "Russian", "ar": "Arabic",
		"th": "Thai", "vi": "Vietnamese", "id": "Indonesian", "it": "Italian",
	}
	v, ok := table[l]
	return v, ok
}

// minimaxEmotions 是厂商原生枚举全集。
var minimaxEmotions = map[string]bool{
	"happy": true, "sad": true, "angry": true, "fearful": true, "disgusted": true,
	"surprised": true, "calm": true, "fluent": true, "whisper": true,
}

// mapEmotion 按设计文档 7.3 处理情绪：返回 (值, 是否写入)。
func mapEmotion(n *core.Norm, emotion, model string) (string, bool) {
	switch emotion {
	case "":
		return "", false
	case "neutral":
		return "calm", true
	case "excited":
		if strings.HasPrefix(model, "speech-2.6-") {
			return "fluent", true
		}
		n.Drop("emotion", fmt.Sprintf("excited maps to fluent which is only supported by speech-2.6-*, model is %s", model))
		return "", false
	case "fluent":
		if strings.HasPrefix(model, "speech-2.6-") {
			return "fluent", true
		}
		n.Drop("emotion", fmt.Sprintf("fluent is only supported by speech-2.6-*, model is %s", model))
		return "", false
	case "whisper":
		if strings.HasPrefix(model, "speech-2.8-") {
			n.Drop("emotion", "whisper is not supported by speech-2.8-* models")
			return "", false
		}
		return "whisper", true
	}
	if minimaxEmotions[emotion] {
		return emotion, true
	}
	n.Drop("emotion", fmt.Sprintf("%q is not a supported minimax emotion", emotion))
	return "", false
}

func (m *MiniMax) Build(req core.SpeakRequest) (*provider.RequestSpec, []string, error) {
	n := core.NewNorm(req.OnUnsupported)

	if len([]rune(req.Text)) >= 10000 {
		return nil, nil, core.NewError(core.ErrInvalidText,
			"text too long for minimax (%d chars, limit <10000)", len([]rune(req.Text))).WithProvider("minimax")
	}

	model := req.Model
	if model == "" {
		model = m.model
	}
	if model == "" {
		// 用户未显式指定 model 时，low latency 档切换到 turbo。
		if req.Latency == "low" {
			model = "speech-2.8-turbo"
		} else {
			model = defaultModel
		}
	}

	voiceSetting := map[string]any{
		"voice_id": req.Voice,
		"speed":    n.ClampF("speed", req.Speed, 0.5, 2.0),
		"vol":      n.ClampF("volume", req.Volume, 0.5, 2.0), // 统一范围内直传，倍数量纲一致
		"pitch":    n.ClampI("pitch", req.Pitch, -12, 12),
	}
	if emo, ok := mapEmotion(n, req.Emotion, model); ok {
		voiceSetting["emotion"] = emo
	}

	audioSetting := map[string]any{
		"format":  req.Format, // mp3/wav/pcm/opus/flac 全支持
		"channel": req.Channels,
	}
	if req.SampleRate > 0 {
		audioSetting["sample_rate"] = n.Nearest("sample_rate", req.SampleRate, sampleRates)
	}
	if req.Bitrate > 0 && req.Format == "mp3" {
		audioSetting["bitrate"] = n.Nearest("bitrate", req.Bitrate*1000, []int{32000, 64000, 128000, 256000})
	}

	payload := map[string]any{
		"model":         model,
		"text":          req.Text,
		"stream":        req.Stream,
		"output_format": "hex",
		"voice_setting": voiceSetting,
		"audio_setting": audioSetting,
	}
	if req.Stream {
		// 避免末 chunk 重复携带整段音频。
		payload["stream_options"] = map[string]any{"exclude_aggregated_audio": true}
	}
	if req.Language != "" {
		if lb, ok := languageBoost(req.Language); ok {
			payload["language_boost"] = lb
		} else {
			n.Drop("language", fmt.Sprintf("unknown language code %q for minimax", req.Language))
		}
	}

	if req.Extra != nil {
		payload = core.DeepMerge(payload, req.Extra)
	}
	if err := n.Err(); err != nil {
		return nil, nil, err
	}

	h := http.Header{}
	h.Set("Authorization", "Bearer "+m.apiKey)
	h.Set("Content-Type", "application/json")

	return &provider.RequestSpec{
		Method:  http.MethodPost,
		URL:     m.baseURL + "/v1/t2a_v2",
		Headers: h,
		Body:    payload,
	}, n.Warnings, nil
}

// mapBaseResp 把 base_resp.status_code 映射为统一错误（HTTP 200 内的软错误）。
func mapBaseResp(code int, msg string) *core.Error {
	var e *core.Error
	switch code {
	case 1004:
		e = core.NewError(core.ErrAuth, "%s", msg)
	case 1002, 1039:
		e = core.NewError(core.ErrRateLimit, "%s", msg)
	case 1042, 2013:
		e = core.NewError(core.ErrInvalidText, "%s", msg)
	case 1000, 1001:
		e = core.NewError(core.ErrProvider, "%s", msg)
	default:
		e = core.NewError(core.ErrProvider, "%s", msg)
	}
	return e.WithProvider("minimax").WithProviderCode(fmt.Sprintf("%d", code)).WithHTTP(200)
}

type baseResp struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

type t2aResponse struct {
	Data struct {
		Audio        string `json:"audio"`
		Status       int    `json:"status"`
		SubtitleFile string `json:"subtitle_file"`
	} `json:"data"`
	ExtraInfo struct {
		AudioLength     int64 `json:"audio_length"` // ms
		AudioSize       int64 `json:"audio_size"`
		AudioSampleRate int   `json:"audio_sample_rate"`
		Bitrate         int   `json:"bitrate"` // bps
		AudioChannel    int   `json:"audio_channel"`
		UsageCharacters int   `json:"usage_characters"`
	} `json:"extra_info"`
	TraceID  string   `json:"trace_id"`
	BaseResp baseResp `json:"base_resp"`
}

func (m *MiniMax) Speak(ctx context.Context, req core.SpeakRequest) (*provider.Result, error) {
	spec, warnings, err := m.Build(req)
	if err != nil {
		return nil, err
	}
	resp, err := httpx.Do(ctx, m.client, "minimax", httpx.Spec{Method: spec.Method, URL: spec.URL, Headers: spec.Headers, Body: spec.Body})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		detail := httpx.ReadErrBody(resp)
		var e *core.Error
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			e = core.NewError(core.ErrAuth, "%s", httpx.StatusMsg(resp, detail))
		case resp.StatusCode == http.StatusTooManyRequests:
			e = core.NewError(core.ErrRateLimit, "%s", httpx.StatusMsg(resp, detail))
		case resp.StatusCode >= 500:
			e = core.NewError(core.ErrProvider, "%s", httpx.StatusMsg(resp, detail))
		default:
			e = core.NewError(core.ErrInvalidArgument, "%s", httpx.StatusMsg(resp, detail))
		}
		return nil, e.WithProvider("minimax").WithHTTP(resp.StatusCode)
	}

	stream, _ := spec.Body["stream"].(bool)
	model, _ := spec.Body["model"].(string)
	if stream && strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return m.speakStream(resp, req, model, warnings)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, core.NewError(core.ErrNetwork, "error reading response: %v", err).WithProvider("minimax")
	}
	var parsed t2aResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, core.NewError(core.ErrProvider, "cannot parse minimax response: %v", err).WithProvider("minimax")
	}
	if parsed.BaseResp.StatusCode != 0 {
		return nil, mapBaseResp(parsed.BaseResp.StatusCode, parsed.BaseResp.StatusMsg)
	}
	audio, err := hex.DecodeString(parsed.Data.Audio)
	if err != nil {
		return nil, core.NewError(core.ErrProvider, "cannot hex-decode audio: %v", err).WithProvider("minimax")
	}

	res := &provider.Result{
		Audio:      io.NopCloser(bytes.NewReader(audio)),
		Model:      model,
		Format:     req.Format,
		SampleRate: parsed.ExtraInfo.AudioSampleRate,
		Bitrate:    parsed.ExtraInfo.Bitrate / 1000,
		Channels:   parsed.ExtraInfo.AudioChannel,
		DurationMS: parsed.ExtraInfo.AudioLength,
		Usage:      provider.Usage{Characters: parsed.ExtraInfo.UsageCharacters},
		RequestID:  parsed.TraceID,
		Warnings:   warnings,
	}
	if parsed.Data.SubtitleFile != "" {
		res.Extras = map[string]any{"subtitle_url": parsed.Data.SubtitleFile}
	}
	return res, nil
}

// speakStream 逐 SSE 事件解码音频写入 pipe；status==2 的结束事件补全元数据。
func (m *MiniMax) speakStream(resp *http.Response, req core.SpeakRequest, model string, warnings []string) (*provider.Result, error) {
	pr, pw := io.Pipe()
	done := make(chan struct{})
	res := &provider.Result{
		Audio:    pr,
		Model:    model,
		Format:   req.Format,
		Channels: req.Channels,
		Warnings: warnings,
	}
	res.Wait = func() { <-done }

	go func() {
		defer close(done)
		defer resp.Body.Close()
		err := parseSSE(resp.Body, func(data []byte) error {
			var ev t2aResponse
			if err := json.Unmarshal(data, &ev); err != nil {
				return core.NewError(core.ErrProvider, "cannot parse SSE event: %v", err).WithProvider("minimax")
			}
			if ev.BaseResp.StatusCode != 0 {
				return mapBaseResp(ev.BaseResp.StatusCode, ev.BaseResp.StatusMsg)
			}
			switch ev.Data.Status {
			case 1: // 音频分片
				chunk, err := hex.DecodeString(ev.Data.Audio)
				if err != nil {
					return core.NewError(core.ErrProvider, "cannot hex-decode audio chunk: %v", err).WithProvider("minimax")
				}
				if _, err := pw.Write(chunk); err != nil {
					return err
				}
			case 2: // 结束事件：读取 extra_info（已排除聚合音频，忽略其 audio 字段）
				res.DurationMS = ev.ExtraInfo.AudioLength
				res.SampleRate = ev.ExtraInfo.AudioSampleRate
				res.Bitrate = ev.ExtraInfo.Bitrate / 1000
				if ev.ExtraInfo.AudioChannel > 0 {
					res.Channels = ev.ExtraInfo.AudioChannel
				}
				res.Usage = provider.Usage{Characters: ev.ExtraInfo.UsageCharacters}
				if ev.TraceID != "" {
					res.RequestID = ev.TraceID
				}
			}
			return nil
		})
		pw.CloseWithError(err) // err 为 nil 时正常 EOF
	}()
	return res, nil
}

// ListVoices 调 POST /v1/get_voice 列出系统音色与克隆音色。
func (m *MiniMax) ListVoices(ctx context.Context) ([]provider.Voice, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+m.apiKey)
	h.Set("Content-Type", "application/json")
	resp, err := httpx.Do(ctx, m.client, "minimax", httpx.Spec{
		Method: http.MethodPost, URL: m.baseURL + "/v1/get_voice", Headers: h,
		Body: map[string]any{"voice_type": "all"},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, core.NewError(core.ErrProvider, "unexpected HTTP %d from minimax", resp.StatusCode).WithProvider("minimax").WithHTTP(resp.StatusCode)
	}
	var body struct {
		SystemVoice []struct {
			VoiceID     string   `json:"voice_id"`
			VoiceName   string   `json:"voice_name"`
			Description []string `json:"description"`
		} `json:"system_voice"`
		VoiceCloning []struct {
			VoiceID     string   `json:"voice_id"`
			Description []string `json:"description"`
		} `json:"voice_cloning"`
		BaseResp baseResp `json:"base_resp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, core.NewError(core.ErrProvider, "cannot parse get_voice response: %v", err).WithProvider("minimax")
	}
	if body.BaseResp.StatusCode != 0 {
		return nil, mapBaseResp(body.BaseResp.StatusCode, body.BaseResp.StatusMsg)
	}
	var out []provider.Voice
	for _, v := range body.SystemVoice {
		out = append(out, provider.Voice{ID: v.VoiceID, Name: v.VoiceName, Description: strings.Join(v.Description, "; ")})
	}
	for _, v := range body.VoiceCloning {
		out = append(out, provider.Voice{ID: v.VoiceID, Name: "(cloned)", Description: strings.Join(v.Description, "; ")})
	}
	return out, nil
}

// Check 用 get_voice 接口校验密钥。
func (m *MiniMax) Check(ctx context.Context) error {
	_, err := m.ListVoices(ctx)
	return err
}

var _ provider.Provider = (*MiniMax)(nil)
