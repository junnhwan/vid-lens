package service

import (
	"context"
	"fmt"
	"net"
	neturl "net/url"
	"strings"

	"vid-lens/internal/config"
)

var defaultAllowedVideoHosts = []string{
	"bilibili.com",
	"b23.tv",
	"youtube.com",
	"youtu.be",
}

type remoteURLResolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

type netRemoteURLResolver struct{}

func (netRemoteURLResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

type checkedRemoteVideoURL struct {
	Raw       string
	Sanitized string
	Host      string
}

type remoteVideoURLValidator struct {
	allowedHosts []string
	resolver     remoteURLResolver
}

func newRemoteVideoURLValidator(tools config.ToolsConfig, resolver remoteURLResolver) remoteVideoURLValidator {
	allowed := tools.AllowedVideoHosts
	if len(allowed) == 0 {
		allowed = defaultAllowedVideoHosts
	}
	if resolver == nil {
		resolver = netRemoteURLResolver{}
	}
	return remoteVideoURLValidator{allowedHosts: allowed, resolver: resolver}
}

func (v remoteVideoURLValidator) validate(ctx context.Context, rawURL string) (checkedRemoteVideoURL, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return checkedRemoteVideoURL{}, fmt.Errorf("视频链接格式错误")
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return checkedRemoteVideoURL{}, fmt.Errorf("仅支持 http/https 视频链接")
	}

	host := normalizeHost(parsed.Hostname())
	if host == "" {
		return checkedRemoteVideoURL{}, fmt.Errorf("视频链接缺少 host")
	}
	if host == "localhost" {
		return checkedRemoteVideoURL{}, fmt.Errorf("不允许访问本地地址")
	}
	if !hostAllowed(host, v.allowedHosts) {
		return checkedRemoteVideoURL{}, fmt.Errorf("不支持的视频平台域名: %s", host)
	}

	if ip := net.ParseIP(host); ip != nil {
		if unsafeIP(ip) {
			return checkedRemoteVideoURL{}, fmt.Errorf("不允许访问内网或本地地址")
		}
	} else {
		ips, err := v.resolver.LookupIP(ctx, host)
		if err != nil {
			return checkedRemoteVideoURL{}, fmt.Errorf("解析视频链接域名失败: %w", err)
		}
		if len(ips) == 0 {
			return checkedRemoteVideoURL{}, fmt.Errorf("视频链接域名没有可用解析结果")
		}
		for _, ip := range ips {
			if unsafeIP(ip) {
				return checkedRemoteVideoURL{}, fmt.Errorf("视频链接域名解析到内网或本地地址")
			}
		}
	}

	sanitized := sanitizeRemoteVideoURL(*parsed)
	return checkedRemoteVideoURL{Raw: rawURL, Sanitized: sanitized, Host: host}, nil
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func hostAllowed(host string, allowedHosts []string) bool {
	host = normalizeHost(host)
	for _, allowed := range allowedHosts {
		allowed = normalizeHost(allowed)
		if allowed == "" {
			continue
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func unsafeIP(ip net.IP) bool {
	return ip == nil ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}

func sanitizeRemoteVideoURL(parsed neturl.URL) string {
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
