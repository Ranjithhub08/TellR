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

func translateServer(t *testing.T, reply string, seen *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if seen != nil && len(body.Messages) > 0 {
			*seen = body.Messages[0].Content
		}
		out, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": reply}}}})
		w.Write(out)
	}))
}

func TestTranslateHindi(t *testing.T) {
	var system string
	srv := translateServer(t, " \"250 रुपये दर्ज किए गए।\" ", &system)
	defer srv.Close()
	c := NewClient(srv.URL, "k", "m")
	out, err := c.Translate(context.Background(), "Added 250 rupees for lunch.", "hi")
	if err != nil || out != "250 रुपये दर्ज किए गए।" {
		t.Fatalf("got %q %v", out, err)
	}
	if !strings.Contains(system, "Hindi") || !strings.Contains(system, "Devanagari") {
		t.Fatalf("system prompt: %s", system)
	}
}

func TestTranslateRejectsBadOutput(t *testing.T) {
	for _, reply := range []string{"", "Added 250 rupees", strings.Repeat("हा", 500)} {
		srv := translateServer(t, reply, nil)
		c := NewClient(srv.URL, "k", "m")
		_, err := c.Translate(context.Background(), "Added 250 rupees.", "hi")
		srv.Close()
		if !errors.Is(err, ErrBadOutput) {
			t.Errorf("%q: got %v", reply, err)
		}
	}
}

func TestTranslateGuards(t *testing.T) {
	c := NewClient("http://x", "", "m")
	if _, err := c.Translate(context.Background(), "hi", "hi"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no key: %v", err)
	}
	c = NewClient("http://x", "k", "m")
	if _, err := c.Translate(context.Background(), "hi", "fr"); err == nil {
		t.Fatal("unsupported language accepted")
	}
	if _, err := c.Translate(context.Background(), "  ", "hi"); !errors.Is(err, ErrBadOutput) {
		t.Fatalf("empty: %v", err)
	}
	if !Translatable("te") || Translatable("en") {
		t.Fatal("Translatable")
	}
}
