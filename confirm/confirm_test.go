package confirm

import (
	"testing"
	"time"
)

func fixed(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestTokenIsSingleUse(t *testing.T) {
	s := New(2 * time.Minute)
	token, _, err := s.Put([]Item{{Kind: KindExpense, ID: 7, Label: "lunch"}})
	if err != nil {
		t.Fatal(err)
	}
	items, ok := s.Take(token)
	if !ok || len(items) != 1 || items[0].ID != 7 {
		t.Fatalf("first take failed: %v %v", items, ok)
	}
	if _, ok := s.Take(token); ok {
		t.Fatal("token was usable twice")
	}
}

func TestTokenExpires(t *testing.T) {
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	s := New(2 * time.Minute)
	s.SetClock(fixed(base))
	token, _, _ := s.Put([]Item{{Kind: KindReminder, ID: 1, Label: "call"}})
	s.SetClock(fixed(base.Add(2 * time.Minute)))
	if _, ok := s.Take(token); ok {
		t.Fatal("expired token was accepted")
	}
}

func TestUnknownTokenRejected(t *testing.T) {
	s := New(time.Minute)
	if _, ok := s.Take("nope"); ok {
		t.Fatal("unknown token accepted")
	}
}

func TestEmptyItemsRejected(t *testing.T) {
	s := New(time.Minute)
	if _, _, err := s.Put(nil); err == nil {
		t.Fatal("empty put accepted")
	}
}

func TestDropRemovesToken(t *testing.T) {
	s := New(time.Minute)
	token, _, _ := s.Put([]Item{{Kind: KindExpense, ID: 1}})
	s.Drop(token)
	if _, ok := s.Take(token); ok {
		t.Fatal("dropped token still valid")
	}
}

func TestCapacityIsBounded(t *testing.T) {
	s := New(time.Hour)
	for i := 0; i < maxPending+50; i++ {
		if _, _, err := s.Put([]Item{{Kind: KindExpense, ID: int64(i + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	if s.Len() > maxPending {
		t.Fatalf("store grew to %d", s.Len())
	}
}

func TestPutCopiesItems(t *testing.T) {
	s := New(time.Minute)
	in := []Item{{Kind: KindExpense, ID: 1, Label: "a"}}
	token, _, _ := s.Put(in)
	in[0].ID = 99
	items, _ := s.Take(token)
	if items[0].ID != 1 {
		t.Fatal("stored items alias the caller slice")
	}
}
