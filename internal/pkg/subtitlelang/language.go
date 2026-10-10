// Package subtitlelang compares declared subtitle languages without rewriting
// provider metadata or inferring whether a subtitle is manual or automatic.
package subtitlelang

import "strings"

func Matches(actual, wanted string) bool {
	return actual != "" && wanted != "" && base(actual) == base(wanted)
}

func base(language string) string {
	language = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(language), "_", "-"))
	for _, prefix := range []string{"ai-", "auto-"} {
		if !strings.HasPrefix(language, prefix) {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(language, prefix), "-")
		// Only the declared zh/en provider families are recognized. Unknown
		// prefixed labels stay opaque, never becoming a default language.
		if parts[0] != "zh" && parts[0] != "en" {
			return language
		}
		for _, part := range parts[1:] {
			if len(part) < 2 || len(part) > 8 {
				return language
			}
			for _, char := range part {
				if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9') {
					return language
				}
			}
		}
		return parts[0]
	}
	return strings.SplitN(language, "-", 2)[0]
}
