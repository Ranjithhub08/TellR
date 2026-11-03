package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func speechServer(t *testing.T, status int, body string, seen *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("bad request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		if seen != nil {
			json.NewDecoder(r.Body).Decode(seen)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestSpeakSendsExpectedBody(t *testing.T) {
	var seen map[string]any
	srv := speechServer(t, 200, "RIFFdata", &seen)
	defer srv.Close()
	s := NewSpeech(srv.URL, "k", "m", "hannah")
	audio, err := s.Speak(context.Background(), "hello", "daniel")
	if err != nil || string(audio) != "RIFFdata" {
		t.Fatalf("speak: %q %v", audio, err)
	}
	if seen["model"] != "m" || seen["input"] != "hello" || seen["voice"] != "daniel" || seen["response_format"] != "wav" {
		t.Fatalf("body: %v", seen)
	}
}

func TestSpeakFallsBackToDefaultVoice(t *testing.T) {
	var seen map[string]any
	srv := speechServer(t, 200, "x", &seen)
	defer srv.Close()
	s := NewSpeech(srv.URL, "k", "m", "hannah")
	if _, err := s.Speak(context.Background(), "hi", "not-a-voice"); err != nil {
		t.Fatal(err)
	}
	if seen["voice"] != "hannah" {
		t.Fatalf("voice: %v", seen["voice"])
	}
}

func TestSpeakErrors(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{429, ErrBusy},
		{503, ErrUnavailable},
		{400, ErrSpeechRefused},
	}
	for _, c := range cases {
		srv := speechServer(t, c.status, `{"error":"model_terms_required"}`, nil)
		s := NewSpeech(srv.URL, "k", "m", "hannah")
		_, err := s.Speak(context.Background(), "hi", "")
		srv.Close()
		if !errors.Is(err, c.want) {
			t.Errorf("status %d: got %v want %v", c.status, err, c.want)
		}
	}
}

func TestSpeakNeedsKey(t *testing.T) {
	s := NewSpeech("http://x", "", "m", "hannah")
	if s.Enabled() {
		t.Fatal("should be disabled")
	}
	if _, err := s.Speak(context.Background(), "hi", ""); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v", err)
	}
}

func TestSpeakEmptyAudioIsBadOutput(t *testing.T) {
	srv := speechServer(t, 200, "", nil)
	defer srv.Close()
	s := NewSpeech(srv.URL, "k", "m", "hannah")
	if _, err := s.Speak(context.Background(), "hi", ""); !errors.Is(err, ErrBadOutput) {
		t.Fatalf("got %v", err)
	}
}

func TestValidVoice(t *testing.T) {
	if !ValidVoice("autumn") || ValidVoice("") || ValidVoice(strings.ToUpper("autumn")) {
		t.Fatal("voice validation")
	}
}
