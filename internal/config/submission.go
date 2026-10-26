package config

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Burst is the bucket capacity; Every is the time needed to refill ONE token.
type TokenBucket struct {
	Burst int
	Every time.Duration
}

type SubmissionPolicy struct{ Session, IP, Global TokenBucket }
type SubmissionLimits struct{ Generation, Question SubmissionPolicy }

func DefaultSubmissionLimits() SubmissionLimits {
	return SubmissionLimits{
		Generation: SubmissionPolicy{TokenBucket{3, 10 * time.Minute}, TokenBucket{6, 5 * time.Minute}, TokenBucket{12, 2 * time.Minute}},
		Question:   SubmissionPolicy{TokenBucket{6, 10 * time.Second}, TokenBucket{20, 3 * time.Second}, TokenBucket{60, time.Second}},
	}
}

func (b TokenBucket) Valid() bool {
	return b.Burst >= 1 && b.Burst <= 1000 && b.Every >= time.Millisecond && b.Every <= 24*time.Hour
}

func (l SubmissionLimits) Valid() bool {
	for _, p := range []SubmissionPolicy{l.Generation, l.Question} {
		for _, b := range []TokenBucket{p.Session, p.IP, p.Global} {
			if !b.Valid() {
				return false
			}
		}
	}
	return true
}

func loadSubmissionLimits(get func(string) string) (SubmissionLimits, []netip.Prefix, error) {
	l := DefaultSubmissionLimits()
	for _, action := range []struct {
		name   string
		policy *SubmissionPolicy
	}{{"GENERATION", &l.Generation}, {"QUESTION", &l.Question}} {
		for _, dim := range []struct {
			name   string
			bucket *TokenBucket
		}{{"SESSION", &action.policy.Session}, {"IP", &action.policy.IP}, {"GLOBAL", &action.policy.Global}} {
			key := "VIDLENS_" + action.name + "_" + dim.name + "_RATE"
			if value := get(key); value != "" {
				parts := strings.Split(value, "/")
				if len(parts) != 2 {
					return l, nil, fmt.Errorf("%s must be burst/refill-duration", key)
				}
				n, e1 := strconv.Atoi(parts[0])
				duration, e2 := time.ParseDuration(parts[1])
				b := TokenBucket{n, duration}
				if e1 != nil || e2 != nil || !b.Valid() {
					return l, nil, fmt.Errorf("invalid %s", key)
				}
				*dim.bucket = b
			}
		}
	}
	var trusted []netip.Prefix
	if value := get("VIDLENS_TRUSTED_PROXY_CIDRS"); value != "" {
		for _, cidr := range strings.Split(value, ",") {
			p, err := netip.ParsePrefix(strings.TrimSpace(cidr))
			if err != nil {
				return l, nil, fmt.Errorf("invalid trusted proxy CIDR")
			}
			trusted = append(trusted, p.Masked())
		}
	}
	return l, trusted, nil
}
