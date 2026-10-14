package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterBucketsAndClientIP(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	proxies := mustNets(t, "10.0.0.0/8", "104.16.0.0/13", "2606:4700::/32")
	limiter := New(clock, proxies)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "abc")
		Middleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(w, r)
	})

	for i := 0; i < 120; i++ {
		rec := hit(handler, "203.0.113.9:1", "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("allow %d status %d", i, rec.Code)
		}
	}
	denied := hit(handler, "203.0.113.9:1", "198.51.100.10")
	if denied.Code != http.StatusTooManyRequests || denied.Header().Get("Retry-After") != "1" {
		t.Fatalf("denied %d retry %q", denied.Code, denied.Header().Get("Retry-After"))
	}
	if denied.Body.String() != `{"error":{"code":"rate_limited","message":"Too many requests.","request_id":"abc"}}`+"\n" {
		t.Fatalf("body %s", denied.Body.String())
	}
	if denied.Header().Get("X-Request-ID") != "abc" {
		t.Fatalf("request id %q", denied.Header().Get("X-Request-ID"))
	}
	other := hit(handler, "203.0.113.10:1", "")
	if other.Code != http.StatusNoContent {
		t.Fatalf("other %d", other.Code)
	}

	now = now.Add(500 * time.Millisecond)
	if hit(handler, "203.0.113.9:1", "").Code != http.StatusNoContent {
		t.Fatal("refill")
	}

	mapped := New(clock, nil)
	mappedHandler := Middleware(mapped)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := 0; i < 120; i++ {
		if hit(mappedHandler, "[::ffff:203.0.113.9]:1", "").Code != http.StatusNoContent {
			t.Fatal("mapped allow")
		}
	}
	if hit(mappedHandler, "203.0.113.9:2", "").Code != http.StatusTooManyRequests {
		t.Fatal("mapped addresses did not share a bucket")
	}

	walk := New(clock, proxies)
	walkHandler := Middleware(walk)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	pinned := "203.0.113.5, 198.51.100.10, 104.16.0.1"
	for i := 0; i < 120; i++ {
		if hit(walkHandler, "10.21.157.68:443", pinned).Code != http.StatusNoContent {
			t.Fatal("pinned allow")
		}
	}
	if hit(walkHandler, "10.21.157.68:443", pinned).Code != http.StatusTooManyRequests {
		t.Fatal("pinned should be exhausted")
	}
	if hit(walkHandler, "10.21.157.68:443", "198.51.100.11, 104.16.0.1").Code != http.StatusNoContent {
		t.Fatal("leftmost forged address was used as the key")
	}
	if hit(walkHandler, "203.0.113.8:1234", "198.51.100.10").Code != http.StatusNoContent {
		t.Fatal("untrusted peer used the forwarded header")
	}

	spaced := "203.0.113.5, 198.51.100.10 , 104.16.0.1"
	if hit(walkHandler, "10.21.157.68:443", spaced).Code != http.StatusTooManyRequests {
		t.Fatal("spaces changed the client")
	}
	garbage := "198.51.100.20, not-an-ip, 104.16.0.1"
	for i := 0; i < 120; i++ {
		if hit(walkHandler, "10.21.157.68:443", garbage).Code != http.StatusNoContent {
			t.Fatal("garbage allow")
		}
	}
	if hit(walkHandler, "10.21.157.68:443", "198.51.100.20").Code != http.StatusTooManyRequests {
		t.Fatal("unparseable hop became the key")
	}

	v6 := "2001:db8::1, 2606:4700:4700::1111"
	for i := 0; i < 120; i++ {
		if hit(walkHandler, "10.0.0.1:443", v6).Code != http.StatusNoContent {
			t.Fatal("ipv6 allow")
		}
	}
	if hit(walkHandler, "10.0.0.1:443", "2001:db8::1").Code != http.StatusTooManyRequests {
		t.Fatal("cloudflare v6 hop was used as the key")
	}
	if hit(walkHandler, "10.0.0.1:443", "2001:db8::2").Code != http.StatusNoContent {
		t.Fatal("second ipv6 client")
	}

	peer := New(func() time.Time { return now }, proxies)
	peerHandler := Middleware(peer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := 0; i < 120; i++ {
		if hit(peerHandler, "10.1.1.1:10", "").Code != http.StatusNoContent {
			t.Fatal("empty header allow")
		}
	}
	if hit(peerHandler, "10.1.1.1:11", "104.16.0.1").Code != http.StatusTooManyRequests {
		t.Fatal("trusted-only header did not use the peer")
	}

	health := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	health.RemoteAddr = "203.0.113.9:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, health)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("health %d", rec.Code)
	}
}

func TestLimiterCapacity(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	limiter := New(clock, nil)
	handler := Middleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := 0; i < 10000; i++ {
		if hit(handler, uniqueClient(i), "").Code != http.StatusNoContent {
			t.Fatalf("fill %d", i)
		}
	}
	full := hit(handler, "198.51.100.50:1", "")
	if full.Code != http.StatusTooManyRequests || full.Header().Get("Retry-After") != "60" {
		t.Fatalf("full %d retry %q", full.Code, full.Header().Get("Retry-After"))
	}
	if hit(handler, uniqueClient(0), "").Code != http.StatusNoContent {
		t.Fatal("existing key was evicted while fresh")
	}

	now = now.Add(3 * time.Minute)
	if hit(handler, "198.51.100.50:1", "").Code != http.StatusNoContent {
		t.Fatal("idle keys were not evicted")
	}
}

func uniqueClient(i int) string {
	return fmt.Sprintf("203.%d.%d.%d:1", (i>>16)&0xff, (i>>8)&0xff, i&0xff)
}

func hit(handler http.Handler, remote, forwarded string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = remote
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func mustNets(t *testing.T, cidrs ...string) []net.IPNet {
	t.Helper()
	var nets []net.IPNet
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		nets = append(nets, *network)
	}
	return nets
}
