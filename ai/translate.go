package ai

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var languageNames = map[string][2]string{
	"hi": {"Hindi", "Devanagari"},
	"te": {"Telugu", "Telugu"},
}

func Translatable(code string) bool {
	_, ok := languageNames[code]
	return ok
}

func (c *Client) Translate(ctx context.Context, text, code string) (string, error) {
	if !c.Enabled() {
		return "", ErrNotConfigured
	}
	names, ok := languageNames[code]
	if !ok {
		return "", errors.New("ai: unsupported language")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrBadOutput
	}
	system := "You translate short spoken replies from a money-tracking voice assistant into " + names[0] + ". " +
		"Write in " + names[1] + " script with a natural, polite, conversational tone, the way a helpful person would say it aloud. " +
		"Keep every number, amount, date, time and name exactly as given, and say rupees as the local word for rupees. " +
		"Keep it as short as the original. Reply with the translation only, no quotes, no notes."
	out, err := c.chat(ctx, []message{
		{Role: "system", Content: system},
		{Role: "user", Content: text},
	})
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(strings.Trim(strings.TrimSpace(out), "\"'`"))
	if out == "" || utf8.RuneCountInString(out) > 6*utf8.RuneCountInString(text)+40 {
		return "", ErrBadOutput
	}
	hasLetters := false
	for _, r := range out {
		if unicode.IsLetter(r) && r > unicode.MaxASCII {
			hasLetters = true
			break
		}
	}
	if !hasLetters {
		return "", ErrBadOutput
	}
	return out, nil
}
