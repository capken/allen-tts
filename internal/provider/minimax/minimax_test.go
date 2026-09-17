package minimax

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/capken/allen-tts/internal/core"
)

func baseReq() core.SpeakRequest {
	return core.SpeakRequest{
		Provider: "minimax", Text: "你好", Voice: "male-qn-qingse",
		Speed: 1.0, Volume: 1.0, Format: "mp3", Channels: 1,
		Latency: "quality", OnUnsupported: "warn",
	}
}

func TestBuildBasic(t *testing.T) {
	m := New(Options{APIKey: "sk-mm"})
	spec, warnings, err := m.Build(baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if spec.URL != "https://api.minimax.cn/v1/t2a_v2" {
		t.Errorf("bad url %s", spec.URL)
	}
	if spec.Body["model"] != "speech-2.8-hd" || spec.Body["output_format"] != "hex" || spec.Body["stream"] != false {
		t.Errorf("bad body: %v", spec.Body)
	}
	vs := spec.Body["voice_setting"].(map[string]any)
	if vs["voice_id"] != "male-qn-qingse" || vs["speed"] != 1.0 || vs["vol"] != 1.0 || vs["pitch"] != 0 {
		t.Errorf("voice_setting wrong: %v", vs)
	}
	as := spec.Body["audio_setting"].(map[string]any)
	if as["format"] != "mp3" || as["channel"] != 1 {
		t.Errorf("audio_setting wrong: %v", as)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings %v", warnings)
	}
}

func TestLatencyLowSwitchesToTurbo(t *testing.T) {
	req := baseReq()
	req.Latency = "low"
	m := New(Options{APIKey: "k"})
	spec, _, _ := m.Build(req)
	if spec.Body["model"] != "speech-2.8-turbo" {
		t.Errorf("model = %v, want speech-2.8-turbo", spec.Body["model"])
	}
	// 显式指定 model 时不切换
	req.Model = "speech-2.8-hd"
	spec, _, _ = m.Build(req)
	if spec.Body["model"] != "speech-2.8-hd" {
		t.Errorf("explicit model must not be overridden")
	}
}

func TestLanguageBoost(t *testing.T) {
	cases := map[string]string{
		"zh": "Chinese", "zh-CN": "Chinese", "zh-HK": "Chinese,Yue", "yue": "Chinese,Yue",
		"en": "English", "en-US": "English", "ja": "Japanese", "auto": "auto",
	}
	m := New(Options{APIKey: "k"})
	for lang, want := range cases {
		req := baseReq()
		req.Language = lang
		spec, _, err := m.Build(req)
		if err != nil {
			t.Fatal(err)
		}
		if spec.Body["language_boost"] != want {
			t.Errorf("lang %s → %v, want %s", lang, spec.Body["language_boost"], want)
		}
	}
	// 未知语言码 drop + warn
	req := baseReq()
	req.Language = "xx-unknown"
	spec, warnings, err := m.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Body["language_boost"]; ok {
		t.Error("unknown language should be dropped")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "language dropped") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestEmotionRules(t *testing.T) {
	m := New(Options{APIKey: "k"})

	get := func(model, emotion string) (string, bool, []string) {
		req := baseReq()
		req.Model = model
		req.Emotion = emotion
		spec, warnings, err := m.Build(req)
		if err != nil {
			t.Fatal(err)
		}
		vs := spec.Body["voice_setting"].(map[string]any)
		v, ok := vs["emotion"].(string)
		return v, ok, warnings
	}

	if v, ok, _ := get("", "neutral"); !ok || v != "calm" {
		t.Errorf("neutral → %v/%v, want calm", v, ok)
	}
	if v, ok, _ := get("speech-2.6-hd", "excited"); !ok || v != "fluent" {
		t.Errorf("excited on 2.6 → %v/%v, want fluent", v, ok)
	}
	if _, ok, w := get("speech-2.8-hd", "excited"); ok || len(w) == 0 {
		t.Errorf("excited on 2.8 should drop+warn, got ok=%v warnings=%v", ok, w)
	}
	if _, ok, w := get("speech-2.8-hd", "whisper"); ok || len(w) == 0 {
		t.Errorf("whisper on 2.8 should drop+warn, got ok=%v w=%v", ok, w)
	}
	if v, ok, _ := get("speech-02-hd", "whisper"); !ok || v != "whisper" {
		t.Errorf("whisper on speech-02 → %v/%v, want passthrough", v, ok)
	}
	if v, ok, _ := get("", "disgusted"); !ok || v != "disgusted" {
		t.Errorf("disgusted (native enum) → %v/%v, want passthrough", v, ok)
	}
	if _, ok, w := get("", "bogus"); ok || len(w) == 0 {
		t.Errorf("bogus emotion should drop+warn")
	}
}

func TestPitchClampAndBitrate(t *testing.T) {
	req := baseReq()
	req.Pitch = 15         // → 12
	req.Bitrate = 100      // *1000 → nearest 128000
	req.SampleRate = 30000 // → nearest 32000
	m := New(Options{APIKey: "k"})
	spec, warnings, err := m.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	vs := spec.Body["voice_setting"].(map[string]any)
	if vs["pitch"] != 12 {
		t.Errorf("pitch = %v, want 12", vs["pitch"])
	}
	as := spec.Body["audio_setting"].(map[string]any)
	if as["bitrate"] != 128000 || as["sample_rate"] != 32000 {
		t.Errorf("audio_setting = %v", as)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "pitch clamped from 15 to 12") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestTextTooLong(t *testing.T) {
	req := baseReq()
	req.Text = strings.Repeat("字", 10000)
	m := New(Options{APIKey: "k"})
	_, _, err := m.Build(req)
	ce, ok := err.(*core.Error)
	if !ok || ce.Code != core.ErrInvalidText {
		t.Fatalf("want invalid_text, got %v", err)
	}
}

func TestStreamPayload(t *testing.T) {
	req := baseReq()
	req.Stream = true
	m := New(Options{APIKey: "k"})
	spec, _, err := m.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Body["stream"] != true {
		t.Error("stream should be true")
	}
	so := spec.Body["stream_options"].(map[string]any)
	if so["exclude_aggregated_audio"] != true {
		t.Errorf("stream_options wrong: %v", so)
	}
}

func TestSpeakNonStream(t *testing.T) {
	audio := []byte("FAKE-AUDIO")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["output_format"] != "hex" {
			t.Errorf("output_format = %v", body["output_format"])
		}
		resp := map[string]any{
			"data": map[string]any{"audio": hex.EncodeToString(audio), "status": 2, "subtitle_file": "https://example.com/sub.srt"},
			"extra_info": map[string]any{
				"audio_length": 9900, "audio_size": len(audio), "audio_sample_rate": 32000,
				"bitrate": 128000, "audio_channel": 1, "usage_characters": 26,
			},
			"trace_id":  "trace-123",
			"base_resp": map[string]any{"status_code": 0, "status_msg": "success"},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	m := New(Options{APIKey: "k", BaseURL: srv.URL})
	res, err := m.Speak(context.Background(), baseReq())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Audio)
	if string(got) != string(audio) {
		t.Errorf("audio mismatch: %q", got)
	}
	if res.DurationMS != 9900 || res.Usage.Characters != 26 || res.RequestID != "trace-123" {
		t.Errorf("metadata wrong: %+v", res)
	}
	if res.SampleRate != 32000 || res.Bitrate != 128 || res.Channels != 1 {
		t.Errorf("audio meta wrong: %+v", res)
	}
	if res.Extras["subtitle_url"] != "https://example.com/sub.srt" {
		t.Errorf("extras wrong: %v", res.Extras)
	}
}

func TestSpeakSoftError(t *testing.T) {
	// HTTP 200 里的 base_resp 软错误必须被映射（设计文档 7.3）。
	cases := []struct {
		code int
		want core.ErrCode
	}{
		{1004, core.ErrAuth},
		{1002, core.ErrRateLimit},
		{1039, core.ErrRateLimit},
		{1042, core.ErrInvalidText},
		{2013, core.ErrInvalidText},
		{1000, core.ErrProvider},
		{9999, core.ErrProvider},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"base_resp": map[string]any{"status_code": c.code, "status_msg": "boom"},
			})
		}))
		m := New(Options{APIKey: "k", BaseURL: srv.URL})
		_, err := m.Speak(context.Background(), baseReq())
		srv.Close()
		ce, ok := err.(*core.Error)
		if !ok || ce.Code != c.want {
			t.Errorf("base_resp %d: got %v, want %s", c.code, err, c.want)
			continue
		}
		if ce.HTTPStatus != 200 || ce.ProviderCode == "" {
			t.Errorf("base_resp %d: missing http_status/provider_code: %+v", c.code, ce)
		}
	}
}

func TestSpeakStreamSSE(t *testing.T) {
	chunk1 := []byte("PART1-")
	chunk2 := []byte("PART2")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		enc := func(m map[string]any) string {
			b, _ := json.Marshal(m)
			return string(b)
		}
		io.WriteString(w, "data: "+enc(map[string]any{
			"data":      map[string]any{"audio": hex.EncodeToString(chunk1), "status": 1},
			"base_resp": map[string]any{"status_code": 0},
		})+"\n\n")
		io.WriteString(w, "data: "+enc(map[string]any{
			"data":      map[string]any{"audio": hex.EncodeToString(chunk2), "status": 1},
			"base_resp": map[string]any{"status_code": 0},
		})+"\n\n")
		io.WriteString(w, "data: "+enc(map[string]any{
			"data":       map[string]any{"status": 2},
			"extra_info": map[string]any{"audio_length": 5000, "usage_characters": 2, "audio_sample_rate": 32000},
			"trace_id":   "stream-trace",
			"base_resp":  map[string]any{"status_code": 0},
		})+"\n\n")
	}))
	defer srv.Close()
	m := New(Options{APIKey: "k", BaseURL: srv.URL})
	req := baseReq()
	req.Stream = true
	res, err := m.Speak(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(res.Audio)
	if err != nil {
		t.Fatal(err)
	}
	res.Wait()
	if string(got) != "PART1-PART2" {
		t.Errorf("audio = %q", got)
	}
	if res.DurationMS != 5000 || res.RequestID != "stream-trace" || res.Usage.Characters != 2 {
		t.Errorf("stream metadata wrong: %+v", res)
	}
}

func TestParseSSECrossChunkLines(t *testing.T) {
	// 行跨底层 read 边界：用一个每次只吐 7 字节的 reader。
	payload := "data: {\"a\":1}\n\ndata: {\"b\":2}\n\n"
	r := &slowReader{data: []byte(payload), step: 7}
	var events []string
	err := parseSSE(r, func(data []byte) error {
		events = append(events, string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0] != `{"a":1}` || events[1] != `{"b":2}` {
		t.Errorf("events = %v", events)
	}
}

type slowReader struct {
	data []byte
	pos  int
	step int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	end := s.pos + s.step
	if end > len(s.data) {
		end = len(s.data)
	}
	n := copy(p, s.data[s.pos:end])
	s.pos += n
	return n, nil
}
