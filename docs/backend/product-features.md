# Desktop site, models and private outputs

The React application provides `/`, `/example`, `/docs/*` and the hidden owner route `/manage`. Documentation is built from `docs/site/*.md`. The example contains a pre-generated article and its original images, without private Q&A or authenticated URLs; opening it creates no generation task. Theme choices are light, dark and system, persisted locally.

## Owner setup and model revisions

The owner login stays unavailable until a password is set in an interactive local terminal. No default password exists. On Windows run the Linux container command with a TTY:

```sh
docker compose -p YOUR_LOCAL_PROJECT -f compose.backend.yaml exec backend \
  /usr/local/bin/vidlens --config /etc/vidlens/backend.yaml --set-owner-password
```

For a native Linux build use `.local/bin/vidlens --config .local/backend.yaml --set-owner-password`. Resetting the password revokes owner sessions. Owner cookies are separate from visitor cookies; writes require the returned CSRF token and the configured exact Origin. CLI Bearer authentication remains available. Do not enter the Bearer token in browser storage.

In `/manage`, import the current configuration or add a provider and disabled models. Discovery reads the provider's `/models`; importing a candidate never enables or probes it automatically. Run the durable image/tool capability check, then explicitly enable the verified revision. Every enabled, verified model whose provider is enabled appears in the visitor catalog immediately, without requiring a default. The default only chooses the initial selection; if it is unset or unavailable, the first available catalog entry is preselected. Explicit selections and already accepted requests never switch automatically.

Enabling the first catalog model ends the legacy single-model shadow mode. Importing a disabled candidate alone preserves the existing service. Existing databases with enabled catalog entries are recognized even if no catalog default was set. After catalog use begins, stopping all models leaves an empty catalog rather than restoring the legacy model.

Changing credentials, routing, cache policy or model metadata creates immutable revisions. Provider connection changes also create disabled model revisions requiring another check; enable/disable alone retains the effective revision. Old requests retain their original endpoint/key reference and historical model name. Every new physical call also checks the current model/provider gate, so stopping a model prevents further calls without replacing it automatically. Accepted in-flight results can still finish validation.

Probes use a separate durable queue identity and the generation AI lane, with at most three submissions, 512 output tokens per call and a 180-second absolute deadline. Unknown provider outcomes after interruption are retained and never automatically resubmitted. Cache probes report field availability and observed cache reads/creation separately; absent fields and billing remain unknown. Explicit Qwen cache hints are applied only after a successful same-revision probe observes supporting cache evidence.

`POST /api/v1/admin/config/test` now requires `Idempotency-Key` and returns HTTP 202 with a durable probe ID and `status_url`. It imports the current profile as a disabled catalog entry, without changing the live default. Poll the returned status URL; retry the original key after an uncertain HTTP response.

## Visitor contract additions

Read the public `GET /api/v1/models` catalog before starting. `POST /api/v1/requests` accepts optional `model_id`, `model_revision_id` and `purpose`. Purpose normalizes CRLF and surrounding whitespace, with a 500 Unicode-code-point limit. Custom purposes are isolated by anonymous session. Preparation can share compatible source/ASR work; interpretation reuse includes the frozen model and purpose. Retries, conversations and subsequent turns retain the original spec.

Request/article views include a safe `generation_model` and purpose alongside immutable article content. Historical records have `legacy: true` when early Q&A provenance cannot be guaranteed. Public `GET /api/v1/site/stats` counts distinct `(anonymous session, article)` completed-reading receipts, not visits or model calls. `PUT /api/v1/site/like` is session-authenticated, CSRF-protected and idempotent; receipts survive session expiry.

## Article exports

Create an owned export with `POST /api/v1/requests/{id}/exports`, body `{"format":"markdown"}` or `{"format":"markdown_zip"}`, CSRF and a new action key. Read `/exports/latest?format=...` or `/exports/{export}`. Only an accepted ready export provides `/content`; GET never starts work.

Markdown contains the full published structure, captions and source links, with no Cookie-dependent images. ZIP contains `article.md`, exact original image bytes, `sources.json` and a byte/hash manifest. Neither format includes private Q&A, external Q&A references, full transcripts or credentials. Missing or corrupt required images prevent publication of a complete ZIP.

Exports allow one pending task per session, five globally, one executing worker, a 64 MiB file/128-entry cap and a 256 MiB copy pool also counted against temporary storage. They have a 10-minute absolute deadline, two minutes of active work, at most one bounded I/O retry and a 24-hour ready lifetime. Downloads have global/per-session leases and a five-minute connection deadline. Expiry affects the copy, not the article or formal images. Cleanup has priority over pending maintenance work and preserves active downloads.

## Optional web references

Known public HTML reading and Tavily search are independent runtime switches, both off by default:

```yaml
reference_read_enabled: "true"
search_enabled: "false"
tavily_api_key: ""
search_monthly_limit: "1000"
```

Each Q&A turn must explicitly send `allow_external_sources: true`; optional `reference_urls` supplies at most three known URLs. Read tools can only use registered reference IDs. Search is basic only, without automatic parameter selection or paid fallback, with two queries per turn and a conservative monthly reservation capped at 1000 credits. Use a dedicated free-plan key with paid overage disabled at the provider. Unknown results consume the reserved credit; quota/plan limits stop further search. [Tavily parameters](https://docs.tavily.com/documentation/api-reference/endpoint/search) and [credits](https://docs.tavily.com/documentation/api-credits) describe the upstream contract.

Public-page fetching is direct and credential-free, with public DNS bound to the actual dial, redirect checks, 20-second deadlines, 2 MiB compressed/expanded caps and 12,000-character readable snapshots. Search snippets are never citeable evidence. Final answers validate the exact reference ID, final URL, title, read time, content hash and continuous actual quote. Snapshots remain private to their turn/session and expire after 24 hours. Untrusted page text cannot grant tool permissions or alter model routing.

Send `X-VidLens-Contract: 2` on HTTP requests. Returned SSE URLs retain `contract=2`; old clients receive `CLIENT_UPDATE_REQUIRED` before consuming external citations and must stop reconnection until upgraded. This does not change published article schema/content.

Actual provider capability, cache benefit and free-search integration require separate real endpoint checks. Fixture tests do not prove billing, ongoing provider availability or long-video quality.
