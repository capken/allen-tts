package cartesia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/capken/allen-tts/internal/core"
)

func baseReq() core.SpeakRequest {
	return core.SpeakRequest{
		Provider: "cartesia", Text: "Hello", Voice: "voice-uuid",
		Speed: 1.0, Volume: 1.0, Format: "mp3", Channels: 1,
		Latency: "quality", OnUnsupported: "warn",
	}
}

func TestBuildBasicMP3Defaults(t *testing.T) {
	c := New(Options{APIKey: "sk-car-secret"})
	spec, warnings, err := c.Build(baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if spec.URL != "https://api.cartesia.ai/tts/bytes" {
		t.Errorf("bad url %s", spec.URL)
	}
	if got := spec.Headers.Get("Cartesia-Version"); got != "2026-08-14" {
		t.Errorf("Cartesia-Version = %q", got)
	}
	if spec.Body["model_id"] != "sonic-3.6" || spec.Body["transcript"] != "Hello" || spec.Body["voice"] != "voice-uuid" {
		t.Errorf("bad body: %v", spec.Body)
	}
	of := spec.Body["output_format"].(map[string]any)
	if of["container"] != "mp3" || of["sample_rate"] != 44100 || of["bit_rate"] != 128000 {
		t.Errorf("mp3 defaults wrong: %v", of)
	}
	if _, ok := spec.Body["generation_config"]; ok {
		t.Error("generation_config should be omitted at defaults")
	}
	if _, ok := spec.Body["locale"]; ok {
		t.Error("locale must never be written")
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings %v", warnings)
	}
}

func TestSpeedClampAndEmotionMapping(t *testing.T) {
	req := baseReq()
	req.Speed = 1.8         // clamp → 1.5
	req.Emotion = "fearful" // → scared
	req.Volume = 1.1
	c := New(Options{APIKey: "k"})
	spec, warnings, err := c.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	gen := spec.Body["generation_config"].(map[string]any)
	if gen["speed"] != 1.5 {
		t.Errorf("speed = %v, want 1.5", gen["speed"])
	}
	if gen["emotion"] != "scared" {
		t.Errorf("emotion = %v, want scared", gen["emotion"])
	}
	if gen["volume"] != 1.1 {
		t.Errorf("volume = %v, want 1.1", gen["volume"])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "speed clamped from 1.8 to 1.5") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestEmotionPassthrough(t *testing.T) {
	req := baseReq()
	req.Emotion = "sarcastic" // 不在统一枚举 → 原样透传
	c := New(Options{APIKey: "k"})
	spec, _, err := c.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	gen := spec.Body["generation_config"].(map[string]any)
	if gen["emotion"] != "sarcastic" {
		t.Errorf("emotion = %v, want passthrough", gen["emotion"])
	}
}

func TestOldModelNoGenerationConfig(t *testing.T) {
	req := baseReq()
	req.Model = "sonic-2"
	req.Speed = 1.2
	c := New(Options{APIKey: "k"})
	spec, warnings, err := c.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Body["generation_config"]; ok {
		t.Error("generation_config must not be written for pre-sonic-3 models")
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "sonic-3") {
		t.Errorf("expected warning about generation_config, got %v", warnings)
	}
}

func TestSonicLatestGetsGenerationConfig(t *testing.T) {
	req := baseReq()
	req.Model = "sonic-latest"
	req.Speed = 1.2
	c := New(Options{APIKey: "k"})
	spec, _, err := c.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Body["generation_config"]; !ok {
		t.Error("sonic-latest should support generation_config")
	}
}

func TestFormats(t *testing.T) {
	c := New(Options{APIKey: "k"})

	req := baseReq()
	req.Format = "wav"
	req.SampleRate = 23000 // → nearest 22050
	spec, _, err := c.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	of := spec.Body["output_format"].(map[string]any)
	if of["container"] != "wav" || of["encoding"] != "pcm_s16le" || of["sample_rate"] != 22050 {
		t.Errorf("wav output_format wrong: %v", of)
	}

	req.Format = "pcm"
	spec, _, _ = c.Build(req)
	if spec.Body["output_format"].(map[string]any)["container"] != "raw" {
		t.Errorf("pcm should map to raw container")
	}

	for _, bad := range []string{"opus", "flac"} {
		req.Format = bad
		if _, _, err := c.Build(req); err == nil {
			t.Errorf("format %s should error", bad)
		}
	}
}

func TestLanguageAndStreamFallback(t *testing.T) {
	req := baseReq()
	req.Language = "zh-HK"
	req.Stream = true
	c := New(Options{APIKey: "k"})
	spec, warnings, err := c.Build(req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Body["language"] != "zh-HK" {
		t.Errorf("language = %v", spec.Body["language"])
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "streaming") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected streaming fallback warning, got %v", warnings)
	}
}

func TestSpeakErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		code   core.ErrCode
	}{
		{401, core.ErrAuth},
		{403, core.ErrAuth},
		{402, core.ErrQuota},
		{429, core.ErrRateLimit},
		{400, core.ErrInvalidArgument},
		{502, core.ErrProvider},
	}
	for _, cse := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(cse.status)
		}))
		c := New(Options{APIKey: "k", BaseURL: srv.URL})
		_, err := c.Speak(context.Background(), baseReq())
		srv.Close()
		ce, ok := err.(*core.Error)
		if !ok || ce.Code != cse.code {
			t.Errorf("status %d: got %v, want %s", cse.status, err, cse.code)
		}
	}
}
