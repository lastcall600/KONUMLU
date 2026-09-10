package httpx

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPIgnoresXFFWhenUntrusted(t *testing.T) {
	fn := ClientIP(nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:443"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	if got := fn(r); got != "203.0.113.9" {
		t.Fatalf("got %q", got)
	}
}

func TestClientIPUsesXFFOnlyFromTrustedPeer(t *testing.T) {
	_, n, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	fn := ClientIP([]net.IPNet{*n})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.1.2.3:443"
	r.Header.Set("X-Forwarded-For", "198.51.100.20")
	if got := fn(r); got != "198.51.100.20" {
		t.Fatalf("got %q", got)
	}
}

func TestClientIPWalksPastTrustedHops(t *testing.T) {
	_, n, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	fn := ClientIP([]net.IPNet{*n})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.9.9.9:443"
	r.Header.Set("X-Forwarded-For", "198.51.100.20, 10.8.8.8")
	if got := fn(r); got != "198.51.100.20" {
		t.Fatalf("got %q", got)
	}
}

func TestClientIPTrustedPeerWithoutXFFUsesRemoteAddr(t *testing.T) {
	_, n, err := net.ParseCIDR("127.0.0.1/32")
	if err != nil {
		t.Fatal(err)
	}
	fn := ClientIP([]net.IPNet{*n})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:9"
	if got := fn(r); got != "127.0.0.1" {
		t.Fatalf("got %q", got)
	}
}
