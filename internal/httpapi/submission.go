package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"vidlens/internal/domain"
	"vidlens/internal/store"
)

func (a *Server) submissionContext(r *http.Request) context.Context {
	return store.WithSubmissionCheck(r.Context(), func(ctx context.Context, kind, session string) error {
		if a.SubmissionLimiter == nil {
			if a.Config.RedisURL == "" {
				return nil
			} // Minimal internal API fixtures.
			return domain.Err(503, "SUBMISSION_GUARD_UNAVAILABLE", "提交保护暂不可用，请稍后再试。")
		}
		return a.SubmissionLimiter(ctx, kind, session, a.clientIP(r))
	})
}

func (a *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	peer = peer.Unmap()
	trusted := func(ip netip.Addr) bool {
		for _, prefix := range a.Config.TrustedProxyCIDRs {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	// Walk from the known socket peer towards the client. Never trust a header
	// supplied by an untrusted peer, or arbitrary prefixes before that hop.
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(chain) - 1; i >= 0 && trusted(peer); i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil {
			break
		}
		peer = ip.Unmap()
	}
	return peer.String()
}
