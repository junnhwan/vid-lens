# Startup and owner configuration

The backend runs one Go process containing the API, separate generation/Q&A/maintenance Asynq pools, Outbox delivery and reconciliation. It does not serve a frontend. Read `README.md` for prerequisites and test commands.

## New environment

Use `compose.backend.yaml` for fresh, independently named volumes, or connect existing dedicated PostgreSQL/Redis instances. Do not run a second stack on occupied ports. Redis requires `noeviction` and persistent storage. PostgreSQL migrations are embedded and serialized with a database advisory lock.

Copy `config.example.yaml` into an ignored path. Generate two independent random strings of at least 32 characters for `admin_token` and `secret_key`, for example with `openssl rand -hex 32`. Set an absolute external `data_dir`, exact `public_origin` and model settings. The model endpoint is a Chat Completions-compatible base URL ending in `/v1`; the selected model must support images and tools. The ASR endpoint accepts either a base URL or the full `/audio/transcriptions` URL.

```sh
go run ./cmd/vidlens --config .local/backend.yaml --migrate-only
go build -o .local/bin/vidlens ./cmd/vidlens
.local/bin/vidlens --config .local/backend.yaml
```

YAML initializes content configuration and credentials **once per new database**. Later changes use the owner API, so a restart cannot overwrite owner settings with an old file. `VIDLENS_*` environment variables can override flat YAML connection/runtime fields; an environment file is not required. `secret_key` encrypts the separate PostgreSQL credential store and signs cursors/CSRF. Preserve it with backups; changing it alone cannot decrypt existing credentials.

For an optional containerized backend, use the `app` Compose profile after filling `.local/backend.yaml`:

```sh
docker compose -p vidlens-dev -f compose.backend.yaml --profile app up -d --build
```

The container configuration overrides database/Redis addresses, listen address and data directory; the browser origin must still match the proxy configuration. Use Linux/Docker on Windows: the current backend uses Unix system calls and does not build natively there. Configure an independent stack and check occupied ports before starting it.

Bilibili and YouTube acquisition never reads the owner's platform Cookie file or browser login, and has no account fallback. Bilibili reads the public web metadata/playback/subtitle API over a credential-free direct connection, requests the highest quality and selects the highest resolution actually returned (without a fixed 1080p cap). Resource/byte limits still apply. Account-only qualities, paid content and previews are not accepted as complete public videos; inaccessible subtitles fall back to ASR. A new Bilibili acquisition-policy work key prevents new requests from reusing old account-backed source snapshots. YouTube retains the configured extractor proxy and explicitly disables Cookie files and browser Cookie extraction for metadata, subtitles, video and audio. The legacy `platform_cookie_file`/Compose mount remains compatible but is ignored for these sources; no Cookie export is needed on Windows.

Native YouTube execution needs `yt-dlp[default]==2026.8.19`, including the matching `yt-dlp-ejs` package, and either Deno 2.3+ or Node 22+. yt-dlp enables Deno by default; the backend additionally enables Node. Install the packages together, for example `python3 -m pip install 'yt-dlp[default]==2026.8.19'`, in the same Python environment used by the `yt-dlp` executable. The container image includes Deno and the matching Python dependencies, so Windows Docker restoration does not borrow the host browser or require a logged-in YouTube account. Challenge scripts are installed with the image rather than downloaded at job time.

Anonymous acquisition can still be refused by YouTube. Login/robot verification and recognized account restrictions produce `SOURCE_ACCESS_RESTRICTED` with a fixed actionable message; raw extractor stderr, credentials and signed media URLs are not returned to visitors. Private, members-only or age-restricted sources are not promised. A PO Token provider can be evaluated separately if a deployment needs it; this does not turn account-only sources into anonymous content or guarantee access from every server IP.

## Owner API

Send `Authorization: Bearer <admin_token>` to these routes. They do not use the anonymous visitor cookie, expose no private visitor questions and never return API keys.

| Route | Behavior |
| --- | --- |
| `GET /api/v1/admin/config` | Frozen profile, current revision ID, booleans indicating stored keys |
| `PUT /api/v1/admin/config` | `{profile,model_api_key,asr_api_key}`; creates a new immutable profile revision and separately rotates supplied credentials |
| `POST /api/v1/admin/config/test` | `{}` plus `Idempotency-Key`; HTTP 202 durable image/tool probe, returns status URL |
| `GET /api/v1/admin/status` | Run counts and persisted media/frame byte totals |
| `GET /healthz` | Database readiness only; not a model/platform health guarantee |

To edit a profile, read it first, change only desired fields and PUT the complete `profile`. Empty/omitted key strings preserve existing keys. Credential references are server-owned: replacing a key creates a fresh encrypted reference, preserving credentials needed by already accepted revisions. The response returns the stored profile and never a key. Accepted tasks freeze the profile; host admission may apply lower current input limits. Saving configuration does not regenerate articles or extend task deadlines.

Use the model capability check before accepting real traffic. It asks an Eino agent to call an Enhanced Tool and identify actual test pixels. It is a transport/capability check, not a video-quality evaluation.

The hidden `/manage` page uses an independently configured owner password and CSRF-protected owner session. See [model administration and outputs](product-features.md) for interactive setup, catalog activation and optional references. The legacy configuration-test route now queues the same bounded durable probe as the model manager; it no longer makes an untracked synchronous request.

The worker requires streaming Chat Completions with tool-call deltas, in addition to image support. It assembles the complete response internally. Do not disable internal streaming just because the visitor interface displays only finalized answers. The optional Qwen `model_disable_thinking` setting is provider-specific; it does not replace the call deadlines or guarantee a particular latency.

## Sources and development samples

Visitors can submit canonical HTTPS Bilibili `/video/BV…?p=N` or YouTube individual-video URLs, including `youtu.be` and Shorts. Playlists, live streams, short redirect services, arbitrary direct media URLs and local paths are not visitor inputs. Different Bilibili P values keep different source identities. The extractor proxy is an owner-controlled startup setting; Bilibili public API access is direct. Upstream anonymous access can still fail with `SOURCE_ACCESS_RESTRICTED` or `SOURCE_UNAVAILABLE`.

Setting `dev_media_dir` explicitly enables `local://filename.mp4` for development. It only opens a regular file inside that directory after symlink resolution. Optional same-basename VTT/SRT/JSON subtitles are read before ASR. Disable this extension for external access; it is absent from public `supported_platforms`.

```sh
python3 scripts/backend_smoke.py --video local://your-independent-sample.mp4 \
  --ask-frame --output .local/sample-result.json
```

The script tests session/CSRF, idempotent creation, persisted status, final SSE, article, owned JPEG content and a private answer. Model/ASR checks make real provider calls.

## Access and retention

Anonymous sessions last 30 days from creation, without visit-based extension. HTTPS sets Secure cookies; local HTTP does not. Cookie loss/expiry has no account-based recovery in v1, and a fresh session cannot claim old resources. Published articles are retained but readable only through an authorized request; public sharing is not implemented.

Original videos/audio are retained until owner cleanup in `/manage` → 视频与存储. Service video/image quotas are persisted in PostgreSQL and adjustable there; they default to 10/5 GiB. Unselected frames, export copies, formal images, private answers and execution history have separate lifetimes. Physical cleanup follows database pins; deleting Redis keys does not delete files. Back up PostgreSQL, `secret_key` and owned article images together. Restoring unrelated application data requires a separate compatibility assessment.
