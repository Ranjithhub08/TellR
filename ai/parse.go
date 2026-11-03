package ai

import (
	"encoding/json"
	"errors"
	"strings"

	"tellr/actions"
)

var ErrBadOutput = errors.New("the AI reply was not valid JSON")

func ParsePlan(content string) (actions.Plan, error) {
	s := strings.TrimSpace(content)
	if s == "" {
		return actions.Plan{}, ErrBadOutput
	}
	s = stripFence(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return actions.Plan{}, ErrBadOutput
	}
	var plan actions.Plan
	if err := json.Unmarshal([]byte(s[start:end+1]), &plan); err != nil {
		return actions.Plan{}, ErrBadOutput
	}
	if len(plan.Actions) == 0 && strings.TrimSpace(plan.Say) == "" {
		return actions.Plan{}, ErrBadOutput
	}
	return plan, nil
}

func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}
