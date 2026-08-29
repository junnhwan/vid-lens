package service

import (
	"context"

	"vid-lens/internal/config"
	"vid-lens/internal/pkg/remoteurl"
)

// Keep the service-level adapter small so existing MediaService tests and
// dependencies do not need to know where the shared URL policy lives. The
// same policy can now also be used by the download consumer.
type remoteURLResolver = remoteurl.Resolver
type checkedRemoteVideoURL = remoteurl.CheckedURL

type remoteVideoURLValidator struct {
	allowedHosts []string
	resolver     remoteURLResolver
}

func newRemoteVideoURLValidator(tools config.ToolsConfig, resolver remoteURLResolver) remoteVideoURLValidator {
	allowed := tools.AllowedVideoHosts
	if len(allowed) == 0 {
		allowed = remoteurl.DefaultAllowedHosts()
	}
	return remoteVideoURLValidator{allowedHosts: allowed, resolver: resolver}
}

func (v remoteVideoURLValidator) validate(ctx context.Context, rawURL string) (checkedRemoteVideoURL, error) {
	return remoteurl.NewPolicy(v.allowedHosts, v.resolver).Validate(ctx, rawURL)
}
