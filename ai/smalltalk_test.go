package ai

import (
	"strings"
	"testing"
	"time"
)

func TestSmallTalkMatches(t *testing.T) {
	now := time.Date(2026, 11, 4, 15, 45, 0, 0, time.UTC)
	cases := map[string]string{
		"Hello!":                  "good afternoon",
		"hey Tellr, how are you?": "doing great",
		"Thank you so much.":      "welcome",
		"What's your name?":       "TellR",
		"what time is it":         "3:45 PM",
		"What is the time now?":   "3:45 PM",
		"what's the date today":   "Wednesday, 4 November 2026",
		"what day is today":       "Wednesday",
		"my name is ravi":         "Nice to meet you, Ravi",
		"who made you":            "Ranjith",
		"what can you do":         "reminders",
		"नमस्ते":                  "Hello",
		"धन्यवाद":                 "welcome",
		"హాయ్":                    "Hello",
		"ok tellr good night":     "Good night",
		"Good morning":            "Good morning",
		"आप कैसे हैं":             "doing great",
		"ఇప్పుడు టైం ఎంత":         "3:45 PM",
	}
	for in, want := range cases {
		got, ok := SmallTalk(in, now)
		if !ok || !strings.Contains(got, want) {
			t.Errorf("%q -> %q ok=%v, want contains %q", in, got, ok, want)
		}
	}
}

func TestSmallTalkLeavesRealCommandsAlone(t *testing.T) {
	now := time.Now()
	for _, in := range []string{
		"spent 250 on lunch",
		"hello I spent 250 on lunch",
		"remind me to call mom at 6 pm",
		"what is the capital of france",
		"how much did I spend today",
		"thanks for the 200 rupees lunch, add it",
		"",
		"time to pay rent tomorrow",
		"delete the last expense",
	} {
		if got, ok := SmallTalk(in, now); ok {
			t.Errorf("%q should go to the planner, got %q", in, got)
		}
	}
}
