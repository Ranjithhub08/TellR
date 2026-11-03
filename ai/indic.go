package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type IndicSpeech struct {
	BaseURL string
	APIKey  string
	Model   string
	Speaker string
	HTTP    *http.Client
}

func NewIndicSpeech(baseURL, apiKey, model, speaker string) *IndicSpeech {
	return &IndicSpeech{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		Speaker: speaker,
		HTTP:    &http.Client{Timeout: 12 * time.Second},
	}
}

func (s *IndicSpeech) Enabled() bool {
	return s != nil && s.APIKey != "" && s.BaseURL != ""
}

func indicCode(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "hi", "hi-in":
		return "hi-IN"
	case "te", "te-in":
		return "te-IN"
	}
	return ""
}

func (s *IndicSpeech) Speak(ctx context.Context, text, lang string) ([]byte, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	code := indicCode(lang)
	if code == "" {
		return nil, fmt.Errorf("%w: unsupported language", ErrSpeechRefused)
	}
	body := map[string]any{
		"text":               text,
		"language_code":      code,
		"model":              s.Model,
		"pace":               1.0,
		"speech_sample_rate": 24000,
		"output_audio_codec": "wav",
	}
	if s.Speaker != "" {
		body["speaker"] = s.Speaker
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/text-to-speech", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("api-subscription-key", s.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrBusy
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: %d: %s", ErrSpeechRefused, resp.StatusCode, snippet(data))
	}
	var out struct {
		Audios []string `json:"audios"`
	}
	if err := json.Unmarshal(data, &out); err != nil || len(out.Audios) == 0 {
		return nil, ErrBadOutput
	}
	audio, err := base64.StdEncoding.DecodeString(out.Audios[0])
	if err != nil || len(audio) == 0 {
		return nil, ErrBadOutput
	}
	return audio, nil
}
