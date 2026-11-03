package handlers

import (
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) take(now time.Time, perSecond, burst float64) (bool, time.Duration) {
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = math.Min(burst, b.tokens+elapsed*perSecond)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / perSecond * float64(time.Second))
	return false, wait
}

type Limiter struct {
	mu        sync.Mutex
	perSecond float64
	burst     float64
	b         bucket
	now       func() time.Time
}

func NewLimiter(perMinute, burst int) *Limiter {
	now := time.Now()
	return &Limiter{
		perSecond: float64(perMinute) / 60,
		burst:     float64(burst),
		b:         bucket{tokens: float64(burst), last: now},
		now:       time.Now,
	}
}

func (l *Limiter) Allow() (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.take(l.now(), l.perSecond, l.burst)
}

type IPLimiter struct {
	mu        sync.Mutex
	perSecond float64
	burst     float64
	m         map[string]*bucket
	now       func() time.Time
}

const maxTrackedIPs = 5000

func NewIPLimiter(perMinute, burst int) *IPLimiter {
	return &IPLimiter{
		perSecond: float64(perMinute) / 60,
		burst:     float64(burst),
		m:         make(map[string]*bucket),
		now:       time.Now,
	}
}

func (l *IPLimiter) Allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.m[ip]
	if !ok {
		if len(l.m) >= maxTrackedIPs {
			l.purgeLocked(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.m[ip] = b
	}
	return b.take(now, l.perSecond, l.burst)
}

func (l *IPLimiter) purgeLocked(now time.Time) {
	for k, b := range l.m {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.m, k)
		}
	}
	if len(l.m) >= maxTrackedIPs {
		l.m = make(map[string]*bucket)
	}
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func retrySeconds(d time.Duration) int {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return s
}
