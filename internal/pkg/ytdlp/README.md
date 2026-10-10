# Bilibili identity and subtitle adapter

`NewAdapter(Config)` shares configured binary, FFmpeg, proxy and cookie paths.
The legacy `DownloadVideo` remains available. The new import flow uses:

1. `ResolveIdentity(ctx, videoURL)`: ordinary BV/AV plus validated b23 redirects;
   official view metadata supplies aid, BV, selected page CID and duration.
2. `DownloadVideoWithIdentity(ctx, identity)`: actual saved extraction metadata
   must match BV/part/duration; re-read official page mapping must match CID.
   Selected provider stream paths add CID evidence where available. The caller
   hashes and deletes the returned media file.
3. `ListSubtitleTracks(ctx, identity)`: bounded structured metadata, explicit
   `--write-subs`, `all,-danmaku`, SRT only, isolated cleaned temporary directory.
4. `SelectSubtitleTrack` and `FetchSelectedSubtitle`: honor an available explicit
   selection or rank matching language by evidenced kind. Bind track to CID/part.
   Private inline payload and signed fetch URL are excluded from JSON.
5. Pass fetched bytes to `textsource.ParseSRT`; unusable or missing subtitles
   enter the existing ASR path through the service's durable handoff.

The locally inspected executable was **yt-dlp 2026.08.19**. Its installed
`yt_dlp/extractor/bilibili.py` `_get_subtitles` emits inline SRT under `subtitles`,
also emits danmaku there, and drops original provider subtitle-kind metadata.
Consequently membership in `subtitles` is classified as **unknown**, never
automatically manual. Explicit type plus evidence is accepted when present;
an undocumented numeric flag alone is not interpreted as manual proof.

That extractor emits `id = BV…_pN`, or `BV…` for a single default page, and does
not emit a top-level CID in ordinary metadata. `IdentityBasis` therefore states
the minimum evidence: actual extracted BV/part matched the same official page
CID before and after downloading. `ProviderCIDEvidence` additionally identifies
a direct provider CID field or selected `upgcxcode` stream path check. This is
an explicit evidence boundary, not a claim that every codec's bytes embed a CID.

`remoteurl.HTTPClient` applies a server target allowlist on every redirect and
connects directly to the validated public IP, preserving normal TLS hostname
verification. It bounds bodies, headers, redirects and request time. Cookie jar
matching respects host/domain, path, expiry and secure attributes; HTTP errors
omit signed URLs and provider payloads.

Standard HTTP proxy CONNECT cannot guarantee the proxy resolves the same IP as
the client. New identity and URL-subtitle probing explicitly return
`ErrHTTPProxyUnsupported` when a proxy is configured. The external yt-dlp legacy
path retains configured proxy behavior. External yt-dlp's own subsequent
requests still require deployment egress restrictions; admission DNS validation
and these fixture tests do not constitute a complete process network sandbox.

Tests use in-memory metadata, fake command runners and local HTTP servers. They
prove adapter behavior and boundaries without establishing real Bilibili
availability, authentication, media-decoding success or model quality.
