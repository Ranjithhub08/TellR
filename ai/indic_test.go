package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndicSpeechRequestAndDecode(t *testing.T) {
	var got map[string]any
	var key, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, path = r.Header.Get("api-subscription-key"), r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.Write([]byte(`{"request_id":"x","audios":["` + base64.StdEncoding.EncodeToString([]byte("RIFFwave")) + `"]}`))
	}))
	defer srv.Close()
	s := NewIndicSpeech(srv.URL, "k", "bulbul:v3", "priya")
	audio, err := s.Speak(context.Background(), "నమస్కారం", "te")
	if err != nil || string(audio) != "RIFFwave" {
		t.Fatalf("audio=%q err=%v", audio, err)
	}
	if key != "k" || path != "/text-to-speech" || got["language_code"] != "te-IN" || got["model"] != "bulbul:v3" || got["speaker"] != "priya" || got["output_audio_codec"] != "wav" {
		t.Fatalf("key=%q path=%q body=%v", key, path, got)
	}
	if _, err := s.Speak(context.Background(), "hi", "fr"); err == nil {
		t.Fatal("unsupported language must fail")
	}
}

func TestIndicSpeechErrors(t *testing.T) {
	status := http.StatusOK
	body := `{"audios":[]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer srv.Close()
	s := NewIndicSpeech(srv.URL, "k", "m", "")
	ctx := context.Background()
	if _, err := s.Speak(ctx, "x", "hi"); !errors.Is(err, ErrBadOutput) {
		t.Fatalf("empty: %v", err)
	}
	status = 429
	if _, err := s.Speak(ctx, "x", "hi"); !errors.Is(err, ErrBusy) {
		t.Fatalf("429: %v", err)
	}
	status = 503
	if _, err := s.Speak(ctx, "x", "hi"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("503: %v", err)
	}
	status = 403
	if _, err := s.Speak(ctx, "x", "hi"); !errors.Is(err, ErrSpeechRefused) {
		t.Fatalf("403: %v", err)
	}
	var off *IndicSpeech
	if off.Enabled() || NewIndicSpeech("x", "", "m", "").Enabled() {
		t.Fatal("must be disabled without a key")
	}
	if _, err := NewIndicSpeech("x", "", "m", "").Speak(ctx, "x", "hi"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no key: %v", err)
	}
}
