package ratelimit

import (
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type userBucket struct {
	tokens   float64
	refilled time.Time
	lastUsed time.Time
}

type UserLimiter struct {
	mu    sync.Mutex
	clock func() time.Time
	keys  map[string]*userBucket
}

type Reservation struct {
	limiter *UserLimiter
	bucket  *userBucket
	once    sync.Once
}

func NewUserLimiter(clock func() time.Time) *UserLimiter {
	return &UserLimiter{clock: clock, keys: map[string]*userBucket{}}
}

func (l *UserLimiter) Reserve(userID string) (*Reservation, int, bool) {
	now := l.clock()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.keys[userID]
	if bucket == nil {
		l.evictIdleUsers(now)
		if len(l.keys) >= 10000 {
			return nil, 60, false
		}
		bucket = &userBucket{tokens: 60, refilled: now, lastUsed: now}
		l.keys[userID] = bucket
	}
	elapsed := now.Sub(bucket.refilled).Seconds()
	bucket.tokens = math.Min(60, bucket.tokens+elapsed)
	bucket.refilled = now
	bucket.lastUsed = now
	if bucket.tokens < 1 {
		return nil, max(1, int(math.Ceil(1-bucket.tokens))), false
	}
	bucket.tokens--
	return &Reservation{limiter: l, bucket: bucket}, 0, true
}

func (r *Reservation) Commit() {
	r.once.Do(func() {})
}

func (r *Reservation) Cancel() {
	r.once.Do(func() {
		r.limiter.mu.Lock()
		r.bucket.tokens = math.Min(60, r.bucket.tokens+1)
		r.limiter.mu.Unlock()
	})
}

func (l *UserLimiter) evictIdleUsers(now time.Time) {
	for key, bucket := range l.keys {
		if now.Sub(bucket.lastUsed) >= 3*time.Minute {
			delete(l.keys, key)
		}
	}
}

type bucket struct {
	lim  *rate.Limiter
	last time.Time
}

type Limiter struct {
	mu      sync.Mutex
	clock   func() time.Time
	proxies []net.IPNet
	keys    map[string]*bucket
}

func New(clock func() time.Time, proxies []net.IPNet) *Limiter {
	copied := append([]net.IPNet(nil), proxies...)
	return &Limiter{clock: clock, proxies: copied, keys: map[string]*bucket{}}
}

func (l *Limiter) allow(remoteAddr, forwarded string) (int, bool) {
	now := l.clock()
	key := l.clientKey(remoteAddr, forwarded)
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.keys[key]
	if b == nil {
		l.evictIdle(now)
		if len(l.keys) >= 10000 {
			return 60, false
		}
		b = &bucket{lim: rate.NewLimiter(rate.Limit(2), 120), last: now}
		l.keys[key] = b
	}
	b.last = now
	reservation := b.lim.ReserveN(now, 1)
	delay := reservation.DelayFrom(now)
	if delay > 0 {
		reservation.CancelAt(now)
		secs := int((delay + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 1
		}
		return secs, false
	}
	return 0, true
}

func (l *Limiter) evictIdle(now time.Time) {
	for key, b := range l.keys {
		if now.Sub(b.last) >= 3*time.Minute {
			delete(l.keys, key)
		}
	}
}

func (l *Limiter) clientKey(remoteAddr, forwarded string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return remoteAddr
	}
	if !l.trusted(ip) {
		return canonicalIP(ip)
	}
	if client, ok := l.forwardedClient(forwarded); ok {
		return client
	}
	return canonicalIP(ip)
}

func (l *Limiter) forwardedClient(header string) (string, bool) {
	parts := strings.Split(header, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		entry := strings.TrimSpace(parts[i])
		ip := net.ParseIP(entry)
		if ip == nil || l.trusted(ip) {
			continue
		}
		return canonicalIP(ip), true
	}
	return "", false
}

func (l *Limiter) trusted(ip net.IP) bool {
	for _, network := range l.proxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func canonicalIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}
