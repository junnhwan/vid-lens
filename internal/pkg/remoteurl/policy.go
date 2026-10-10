package remoteurl

import (
	"context"
	"fmt"
	"net"
	neturl "net/url"
	"strconv"
	"strings"
)

var defaultAllowedHosts = []string{
	"bilibili.com",
	"b23.tv",
	"youtube.com",
	"youtu.be",
}

type Resolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

type systemResolver struct{}

func (systemResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

type CheckedURL struct {
	Raw       string
	Sanitized string
	Host      string
}

type Policy struct {
	allowedHosts []string
	resolver     Resolver
}

func DefaultAllowedHosts() []string {
	return append([]string(nil), defaultAllowedHosts...)
}

func NewPolicy(allowedHosts []string, resolver Resolver) Policy {
	if len(allowedHosts) == 0 {
		allowedHosts = defaultAllowedHosts
	}
	if resolver == nil {
		resolver = systemResolver{}
	}
	return Policy{allowedHosts: append([]string(nil), allowedHosts...), resolver: resolver}
}

// Validate performs the admission-time allowlist and DNS safety checks. It is
// intentionally not described as a network sandbox: an external downloader
// may resolve redirects or hosts again after this check.
func (p Policy) Validate(ctx context.Context, rawURL string) (CheckedURL, error) {
	p = NewPolicy(p.allowedHosts, p.resolver)
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return CheckedURL{}, fmt.Errorf("视频链接格式错误")
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return CheckedURL{}, fmt.Errorf("仅支持 http/https 视频链接")
	}
	host := NormalizeHost(parsed.Hostname())
	if host == "" {
		return CheckedURL{}, fmt.Errorf("视频链接缺少 host")
	}
	if host == "localhost" {
		return CheckedURL{}, fmt.Errorf("不允许访问本地地址")
	}
	if !HostAllowed(host, p.allowedHosts) {
		return CheckedURL{}, fmt.Errorf("不支持的视频平台域名: %s", host)
	}

	if ip := net.ParseIP(host); ip != nil {
		if UnsafeIP(ip) {
			return CheckedURL{}, fmt.Errorf("不允许访问内网或本地地址")
		}
	} else {
		ips, err := p.resolver.LookupIP(ctx, host)
		if err != nil {
			return CheckedURL{}, fmt.Errorf("解析视频链接域名失败: %w", err)
		}
		if len(ips) == 0 {
			return CheckedURL{}, fmt.Errorf("视频链接域名没有可用解析结果")
		}
		for _, ip := range ips {
			if UnsafeIP(ip) {
				return CheckedURL{}, fmt.Errorf("视频链接域名解析到内网或本地地址")
			}
		}
	}

	sanitized, err := SanitizeChecked(*parsed)
	if err != nil {
		return CheckedURL{}, err
	}
	return CheckedURL{Raw: rawURL, Sanitized: sanitized, Host: host}, nil
}

func NormalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func HostAllowed(host string, allowedHosts []string) bool {
	host = NormalizeHost(host)
	for _, allowed := range allowedHosts {
		allowed = NormalizeHost(allowed)
		if allowed == "" {
			continue
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func UnsafeIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() {
		return true
	}
	for _, block := range nonPublicRanges {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// IsPrivate alone omits shared carrier ranges (including cloud metadata IPs),
// benchmark/documentation networks and reserved addressing. Treat them as
// non-public too; allowlisted hostnames must not turn into those destinations.
var nonPublicRanges = func() []*net.IPNet {
	var blocks []*net.IPNet
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "100::/64", "2001:db8::/32"} {
		_, block, _ := net.ParseCIDR(raw)
		blocks = append(blocks, block)
	}
	return blocks
}()

func Sanitize(parsed neturl.URL) string {
	clean, _ := SanitizeChecked(parsed)
	return clean
}

// BilibiliPart rejects invalid or conflicting p values before tracking query
// parameters are removed. Missing p selects the first part; equal duplicate
// numeric values are harmless and canonicalized to a single parameter.
func BilibiliPart(parsed neturl.URL) (int, error) {
	query, err := neturl.ParseQuery(parsed.RawQuery)
	if err != nil {
		return 0, fmt.Errorf("视频链接查询参数格式错误")
	}
	values, present := query["p"]
	if !present {
		return 1, nil
	}
	part := 0
	for _, value := range values {
		if value == "" {
			return 0, fmt.Errorf("B 站分 P 必须为正整数")
		}
		for _, r := range value {
			if r < '0' || r > '9' {
				return 0, fmt.Errorf("B 站分 P 必须为正整数")
			}
		}
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("B 站分 P 超出有效范围")
		}
		if part != 0 && n != part {
			return 0, fmt.Errorf("B 站分 P 参数冲突")
		}
		part = n
	}
	return part, nil
}

// SanitizeChecked keeps the selected Bilibili part and YouTube watch ID while
// removing tracking values. Admission paths must use this error-returning API.
func SanitizeChecked(parsed neturl.URL) (string, error) {
	parsed.User = nil
	query := parsed.Query()
	var part int
	if HostAllowed(parsed.Hostname(), []string{"bilibili.com", "b23.tv"}) {
		var err error
		part, err = BilibiliPart(parsed)
		if err != nil {
			return "", err
		}
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if isYouTubeWatchURL(parsed) {
		videoID := strings.TrimSpace(query.Get("v"))
		if videoID != "" {
			values := neturl.Values{}
			values.Set("v", videoID)
			parsed.RawQuery = values.Encode()
		}
	}
	if part > 0 {
		if _, present := query["p"]; present {
			values := neturl.Values{}
			values.Set("p", strconv.Itoa(part))
			parsed.RawQuery = values.Encode()
		}
	}
	return parsed.String(), nil
}

func isYouTubeWatchURL(parsed neturl.URL) bool {
	host := NormalizeHost(parsed.Hostname())
	return (host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")) && parsed.EscapedPath() == "/watch"
}
