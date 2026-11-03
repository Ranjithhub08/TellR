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
	"sync/atomic"
	"time"

	"tellr/actions"
	"tellr/models"
)

var (
	ErrNotConfigured = errors.New("the AI key is not configured")
	ErrBusy          = errors.New("the AI service is busy")
	ErrUnavailable   = errors.New("the AI service is unavailable")
)

type Turn struct {
	Role string
	Text string
}

type Request struct {
	Text      string
	TZ        string
	Now       time.Time
	Expenses  []models.Expense
	Reminders []models.Reminder
	History   []Turn
	Custom    []string
}

type Planner interface {
	Plan(ctx context.Context, req Request) (actions.Plan, error)
}

type Client struct {
	BaseURL  string
	APIKey   string
	Model    string
	HTTP     *http.Client
	noEffort atomic.Bool
}

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.APIKey != ""
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *Client) Plan(ctx context.Context, req Request) (actions.Plan, error) {
	if !c.Enabled() {
		return actions.Plan{}, ErrNotConfigured
	}
	text := SanitizeSpeech(req.Text)
	if text == "" {
		return actions.Plan{}, ErrBadOutput
	}
	system := BuildSystemPrompt(req.Now, req.TZ, req.Expenses, req.Reminders) + customBlock(req.Custom) + HistoryBlock(req.History)
	msgs := []message{
		{Role: "system", Content: system},
		{Role: "user", Content: "<speech>" + text + "</speech>"},
	}

	content, err := c.chat(ctx, msgs)
	if err != nil {
		return actions.Plan{}, err
	}
	plan, perr := ParsePlan(content)
	if perr == nil {
		return plan, nil
	}

	msgs = append(msgs,
		message{Role: "assistant", Content: content},
		message{Role: "user", Content: "That was not valid JSON. Reply again with the JSON object only."},
	)
	content, err = c.chat(ctx, msgs)
	if err != nil {
		return actions.Plan{}, err
	}
	return ParsePlan(content)
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) chat(ctx context.Context, msgs []message) (string, error) {
	body := map[string]any{
		"model":                 c.Model,
		"messages":              msgs,
		"temperature":           0.2,
		"max_completion_tokens": 1500,
	}
	useEffort := !c.noEffort.Load()
	if useEffort {
		body["reasoning_effort"] = "low"
	}

	status, data, err := c.post(ctx, body)
	if err != nil {
		return "", err
	}
	if status == http.StatusBadRequest && useEffort && strings.Contains(string(data), "reasoning_effort") {
		c.noEffort.Store(true)
		delete(body, "reasoning_effort")
		status, data, err = c.post(ctx, body)
		if err != nil {
			return "", err
		}
	}
	switch {
	case status == http.StatusTooManyRequests:
		return "", ErrBusy
	case status >= 500:
		return "", fmt.Errorf("%w: status %d", ErrUnavailable, status)
	case status != http.StatusOK:
		return "", fmt.Errorf("ai: provider returned %d: %s", status, snippet(data))
	}

	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("ai: unreadable response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", ErrBadOutput
	}
	return out.Choices[0].Message.Content, nil
}

func (c *Client) post(ctx context.Context, body map[string]any) (int, []byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, fmt.Errorf("%w: %v", ErrUnavailable, ctx.Err())
		}
		return 0, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return resp.StatusCode, data, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
