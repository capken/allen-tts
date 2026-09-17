package fish

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/capken/allen-tts/internal/core"
)

func baseReq() core.SpeakRequest {
	return core.SpeakRequest{
		Provider: "fish", Text: "Hello", Voice: "voice-1",
		Speed: 1.0, Volume: 1.0, Format: "mp3", Channels: 1,
		Latency: "quality", OnUnsupported: "warn",
	}
}

func TestBuildBasic(t *testing.T) {
	f := New(Options{APIKey: "sk-fish-secret"})
	spec, warnings, err := f.Build(baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if spec.URL != "https://api.fish.audio/v1/tts" || spec.Method != "POST" {
		t.Errorf("bad target: %s %s", spec.Method, spec.URL)
	}
	if got := spec.Headers.Get("model"); got != "s2.1-pro" {
		t.Errorf("model header = %q, want s2.1-pro", got)
	}
	if got := spec.Headers.Get("Authorization"); got != "Bearer sk-fish-secret" {
		t.Errorf("bad auth header %q", got)
	}
	if spec.Body["text"] != "Hello" || spec.Body["reference_id"] != "voice-1" {
		t.Errorf("bad body: %v", spec.Body)
	}
	if _, ok := spec.Body["prosody"]; ok {
		t.Error("prosody should be omitted at defaults")
	}
	if spec.Body["latency"] != "normal" {
		t.Errorf("latency = %v, want normal", spec.Body["latency"])
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestVolumeToDB(t *testing.T) {
	// 0.8 倍 → 20*log10(0.8) = -1.938 → -1.9（保留 1 位小数）
	req := baseReq()
	req.Volume = 0.8
	req.Speed = 1.2
	f := New(Options{APIKey: "k"})
	spec, _, err := f.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	pro := spec.Body["prosody"].(map[string]any)
	if pro["volume"] != -1.9 {
		t.Errorf("volume dB = %v, want -1.9", pro["volume"])
	}
	if pro["speed"] != 1.2 {
		t.Errorf("speed = %v, want 1.2", pro["speed"])
	}
}

func TestClampAndDrops(t *testing.T) {
	req := baseReq()
	req.Speed = 3.0     // clamp → 2.0
	req.Pitch = 5       // drop
	req.Emotion = "sad" // drop
	req.Language = "zh" // drop
	req.Channels = 2    // drop
	f := New(Options{APIKey: "k"})
	spec, warnings, err := f.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	pro := spec.Body["prosody"].(map[string]any)
	if pro["speed"] != 2.0 {
		t.Errorf("speed = %v, want 2.0", pro["speed"])
	}
	if len(warnings) != 5 { // speed clamp + pitch + emotion + language + channels
		t.Errorf("want 5 warnings, got %d: %v", len(warnings), warnings)
	}
	joined := strings.Join(warnings, "\n")
	for _, frag := range []string{"speed clamped from 3 to 2", "pitch dropped", "emotion dropped", "language dropped", "channels dropped"} {
		if !strings.Contains(joined, frag) {
			t.Errorf("warnings missing %q: %v", frag, warnings)
		}
	}
}

func TestOnUnsupportedError(t *testing.T) {
	req := baseReq()
	req.Pitch = 5
	req.OnUnsupported = "error"
	f := New(Options{APIKey: "k"})
	_, _, err := f.Build(req)
	var ce *core.Error
	if err == nil {
		t.Fatal("want error")
	}
	if !asCoreErr(err, &ce) || ce.Code != core.ErrInvalidArgument {
		t.Fatalf("want invalid_argument, got %v", err)
	}
}

func TestFlacAlwaysErrors(t *testing.T) {
	for _, policy := range []string{"warn", "error", "drop"} {
		req := baseReq()
		req.Format = "flac"
		req.OnUnsupported = policy
		f := New(Options{APIKey: "k"})
		if _, _, err := f.Build(req); err == nil {
			t.Errorf("policy %s: flac should always error", policy)
		}
	}
}

func TestMultiSpeakerVoiceSplit(t *testing.T) {
	req := baseReq()
	req.Voice = "id-a, id-b"
	f := New(Options{APIKey: "k"})
	spec, _, err := f.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"id-a", "id-b"}
	if !reflect.DeepEqual(spec.Body["reference_id"], want) {
		t.Errorf("reference_id = %v, want %v", spec.Body["reference_id"], want)
	}
}

func TestBitrateAndSampleRate(t *testing.T) {
	req := baseReq()
	req.Bitrate = 100      // → nearest 128
	req.SampleRate = 40000 // mp3 → nearest 44100
	f := New(Options{APIKey: "k"})
	spec, warnings, err := f.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Body["mp3_bitrate"] != 128 {
		t.Errorf("mp3_bitrate = %v, want 128", spec.Body["mp3_bitrate"])
	}
	if spec.Body["sample_rate"] != 44100 {
		t.Errorf("sample_rate = %v, want 44100", spec.Body["sample_rate"])
	}
	if len(warnings) != 2 {
		t.Errorf("want 2 warnings, got %v", warnings)
	}

	req.Format = "opus"
	req.Bitrate = 30 // *1000 → nearest 32000
	req.SampleRate = 0
	spec, _, err = f.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Body["opus_bitrate"] != 32000 {
		t.Errorf("opus_bitrate = %v, want 32000", spec.Body["opus_bitrate"])
	}
}

func TestExtraDeepMerge(t *testing.T) {
	req := baseReq()
	req.Speed = 1.2
	req.Extra = map[string]any{
		"temperature": 0.9,
		"prosody":     map[string]any{"speed": 1.5}, // 对象递归合并，标量覆盖
	}
	f := New(Options{APIKey: "k"})
	spec, _, err := f.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Body["temperature"] != 0.9 {
		t.Errorf("extra scalar not merged: %v", spec.Body)
	}
	pro := spec.Body["prosody"].(map[string]any)
	if pro["speed"] != 1.5 {
		t.Errorf("extra should override prosody.speed: %v", pro)
	}
	if _, ok := pro["volume"]; !ok {
		t.Errorf("deep merge should keep prosody.volume: %v", pro)
	}
}

func TestSpeakErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		code   core.ErrCode
	}{
		{401, core.ErrAuth},
		{402, core.ErrQuota},
		{503, core.ErrRateLimit},
		{500, core.ErrProvider},
		{422, core.ErrInvalidArgument},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
		}))
		f := New(Options{APIKey: "k", BaseURL: srv.URL})
		_, err := f.Speak(context.Background(), baseReq())
		srv.Close()
		var ce *core.Error
		if !asCoreErr(err, &ce) || ce.Code != c.code {
			t.Errorf("status %d: got %v, want code %s", c.status, err, c.code)
		}
	}
}

func TestSpeakSuccess(t *testing.T) {
	audio := []byte("FAKE-MP3-BYTES")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("model") != "s1" {
			t.Errorf("model header = %q", r.Header.Get("model"))
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "allen-tts/") {
			t.Errorf("bad UA %q", ua)
		}
		w.Write(audio)
	}))
	defer srv.Close()
	f := New(Options{APIKey: "k", BaseURL: srv.URL})
	req := baseReq()
	req.Model = "s1"
	res, err := f.Speak(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Audio)
	res.Audio.Close()
	if string(got) != string(audio) {
		t.Errorf("audio mismatch")
	}
	if res.Model != "s1" {
		t.Errorf("model = %q", res.Model)
	}
}

func asCoreErr(err error, target **core.Error) bool {
	ce, ok := err.(*core.Error)
	if ok {
		*target = ce
	}
	return ok
}
