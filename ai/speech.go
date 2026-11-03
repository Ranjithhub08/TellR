package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrSpeechRefused = errors.New("the voice service refused the request")

var SpeechVoices = []string{"hannah", "autumn", "diana", "austin", "daniel", "troy"}

type Speech struct {
	BaseURL string
	APIKey  string
	Model   string
	Voice   string
	HTTP    *http.Client
}

func NewSpeech(baseURL, apiKey, model, voice string) *Speech {
	return &Speech{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		Voice:   voice,
		HTTP:    &http.Client{Timeout: 12 * time.Second},
	}
}

func (s *Speech) Enabled() bool {
	return s != nil && s.APIKey != "" && s.Model != ""
}

func ValidVoice(v string) bool {
	for _, x := range SpeechVoices {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Speech) Speak(ctx context.Context, text, voice string) ([]byte, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	if !ValidVoice(voice) {
		voice = s.Voice
	}
	raw, err := json.Marshal(map[string]any{
		"model":           s.Model,
		"input":           text,
		"voice":           voice,
		"response_format": "wav",
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/audio/speech", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
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
	if len(data) == 0 {
		return nil, ErrBadOutput
	}
	return data, nil
}
