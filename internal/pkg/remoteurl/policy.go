package remoteurl

import (
	"context"
	"fmt"
	"net"
	neturl "net/url"
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

	sanitized := Sanitize(*parsed)
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
	return ip == nil ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}

func Sanitize(parsed neturl.URL) string {
	parsed.User = nil
	query := parsed.Query()
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
	return parsed.String()
}

func isYouTubeWatchURL(parsed neturl.URL) bool {
	host := NormalizeHost(parsed.Hostname())
	return (host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")) && parsed.EscapedPath() == "/watch"
}
