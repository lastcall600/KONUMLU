package httpx

import (
	"net"
	"net/http"
	"strings"
)

const unknownClientIP = "unknown"

// ClientIP returns a function that uses RemoteAddr unless the peer is in a
// trusted proxy CIDR, in which case X-Forwarded-For is walked from the right.
func ClientIP(trusted []net.IPNet) func(*http.Request) string {
	nets := append([]net.IPNet(nil), trusted...)
	return func(r *http.Request) string {
		return clientIP(r, nets)
	}
}

func clientIP(r *http.Request, trusted []net.IPNet) string {
	peer := remoteIP(r)
	if peer == unknownClientIP || len(trusted) == 0 || !ipInNets(peer, trusted) {
		return peer
	}
	fwd := forwardedIPs(r.Header.Get("X-Forwarded-For"))
	if len(fwd) == 0 {
		return peer
	}
	// Walk from the right: skip trusted hops; first untrusted hop is the client.
	for i := len(fwd) - 1; i >= 0; i-- {
		if !ipInNets(fwd[i], trusted) {
			return fwd[i]
		}
	}
	return fwd[0]
}

func remoteIP(r *http.Request) string {
	if r == nil {
		return unknownClientIP
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	addr = strings.Trim(addr, "[]")
	if addr == "" || net.ParseIP(addr) == nil {
		if addr == "" {
			return unknownClientIP
		}
		return addr
	}
	return addr
}

func forwardedIPs(header string) []string {
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if ip := net.ParseIP(part); ip != nil {
			out = append(out, ip.String())
		}
	}
	return out
}

func ipInNets(ip string, nets []net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for i := range nets {
		if nets[i].Contains(parsed) {
			return true
		}
	}
	return false
}
