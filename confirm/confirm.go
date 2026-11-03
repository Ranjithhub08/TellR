package confirm

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const (
	KindExpense  = "expense"
	KindReminder = "reminder"
	maxPending   = 500
)

type Item struct {
	Kind  string `json:"kind"`
	ID    int64  `json:"id"`
	Label string `json:"label"`
}

type entry struct {
	items   []Item
	expires time.Time
}

type Store struct {
	mu  sync.Mutex
	m   map[string]entry
	ttl time.Duration
	now func() time.Time
}

func New(ttl time.Duration) *Store {
	return &Store{m: make(map[string]entry), ttl: ttl, now: time.Now}
}

func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	s.now = now
	s.mu.Unlock()
}

func (s *Store) Put(items []Item) (string, time.Time, error) {
	if len(items) == 0 {
		return "", time.Time{}, errors.New("nothing to confirm")
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.purgeLocked(now)
	if len(s.m) >= maxPending {
		s.evictOldestLocked()
	}
	exp := now.Add(s.ttl)
	cp := make([]Item, len(items))
	copy(cp, items)
	s.m[token] = entry{items: cp, expires: exp}
	return token, exp, nil
}

func (s *Store) Take(token string) ([]Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[token]
	if !ok {
		return nil, false
	}
	delete(s.m, token)
	if !s.now().Before(e.expires) {
		return nil, false
	}
	return e.items, true
}

func (s *Store) Drop(token string) {
	s.mu.Lock()
	delete(s.m, token)
	s.mu.Unlock()
}

func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

func (s *Store) purgeLocked(now time.Time) {
	for k, e := range s.m {
		if !now.Before(e.expires) {
			delete(s.m, k)
		}
	}
}

func (s *Store) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, e := range s.m {
		if first || e.expires.Before(oldest) {
			oldestKey, oldest, first = k, e.expires, false
		}
	}
	if !first {
		delete(s.m, oldestKey)
	}
}
