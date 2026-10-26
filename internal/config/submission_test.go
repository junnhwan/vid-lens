package config

import (
	"testing"
	"time"
)

func TestSubmissionLimitsDefaultsAndValidation(t *testing.T) {
	settings := map[string]string{}
	get := func(k string) string { return settings[k] }
	l, proxies, err := loadSubmissionLimits(get)
	if err != nil || !l.Valid() || len(proxies) != 0 {
		t.Fatal("unsafe defaults", err)
	}
	settings["VIDLENS_GENERATION_SESSION_RATE"] = "4/30s"
	settings["VIDLENS_TRUSTED_PROXY_CIDRS"] = "127.0.0.1/32,::1/128"
	l, proxies, err = loadSubmissionLimits(get)
	if err != nil || l.Generation.Session.Burst != 4 || l.Generation.Session.Every != 30*time.Second || len(proxies) != 2 {
		t.Fatal("configuration not applied", err)
	}
	for _, v := range []string{"0/1s", "1/0s", "-1/1s", "1001/1s", "1/25h", "4", "oops/1s", "1/1ns"} {
		settings["VIDLENS_GENERATION_SESSION_RATE"] = v
		if _, _, err := loadSubmissionLimits(get); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	settings["VIDLENS_GENERATION_SESSION_RATE"] = "1/1s"
	settings["VIDLENS_TRUSTED_PROXY_CIDRS"] = "anything"
	if _, _, err := loadSubmissionLimits(get); err == nil {
		t.Fatal("accepted invalid trusted proxy")
	}
}
