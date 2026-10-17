# Model selection, questions and article exports

The desktop React application provides `/`, `/example` and `/docs/*`. Documentation is built from `docs/site/*.md`. The public examples contain saved articles, original images and selected saved Q&A replays; opening them creates no generation task. Theme choices are light, dark and system, persisted locally.

## Model identity and interpretation

Read the public `GET /api/v1/models` catalog before starting. The home page groups available routes into one visitor choice per exact model version. Display names alone do not merge models. A model name identifies the model, not a promise that its connection is supplied directly by the publisher.

`POST /api/v1/requests` accepts optional `model_id`, `model_revision_id`, `purpose` and `detail_level`. Purpose normalizes CRLF and surrounding whitespace, with a 500 Unicode-code-point limit. Detail is `concise`, `standard` (the default), or `detailed`; see [interpretation detail and recovery](interpretation-and-recovery.md). Custom purposes are isolated by anonymous session. Preparation can share compatible source/ASR work; interpretation reuse includes the frozen generation specification.

Requests, explicit retries, conversations and subsequent turns retain the chosen model identity. On a qualifying connection or provider failure, execution can try another eligible route of that exact model while preserving completed work. It never substitutes a different model version. If no eligible route remains, the request fails explicitly; existing articles remain readable. ASR uses a separate service.

Request/article views include a safe `generation_model`, purpose and detail metadata alongside immutable article content. Historical records have `legacy: true` when early Q&A provenance cannot be guaranteed.

## Public site information

Public `GET /api/v1/site/stats` counts distinct `(anonymous session, article)` completed-reading receipts, not visits or model calls. `PUT /api/v1/site/like` is session-authenticated, CSRF-protected and idempotent; receipts survive session expiry.

Public `GET /api/v1/site/capacity` returns `scope: "video_workspace"`: service quota, retained video/audio and export bytes, outstanding reservations, remaining quota and admission state. It does not expose the host's total/free/used disk bytes or safety margins. Insufficient capacity pauses new downloads; it does not automatically remove retained original media. Published articles and their formal images survive original-media cleanup. See [privacy and retention](../site/privacy.md).

## Article exports

Create an owned export with `POST /api/v1/requests/{id}/exports`, body `{"format":"markdown"}` or `{"format":"markdown_zip"}`, CSRF and a new action key. Read `/exports/latest?format=...` or `/exports/{export}` relative to that request. Only an accepted ready export provides `/content`; GET never starts work.

Markdown contains the full published structure, captions and source links, with no Cookie-dependent images. ZIP contains `article.md`, exact original image bytes, `sources.json` and a byte/hash manifest. Neither format includes private Q&A, external Q&A references, full transcripts or credentials. Missing or corrupt required images prevent publication of a complete ZIP.

Export copies have a 24-hour ready lifetime and a 64 MiB file/128-entry cap. Downloads and export creation are bounded by concurrency, time and storage budgets. Expiry affects the copy, not the article or formal images; a new export can be created later. Creating an export does not call models or regenerate the article.

## Optional web references

Web supplementation is off by default and may be unavailable on a given instance. Each Q&A turn must explicitly send `allow_external_sources: true`; optional `reference_urls` supplies at most three known URLs. Read tools can only use registered reference IDs. If search is unavailable or its allowance is exhausted, video-only Q&A and permitted known-page reading remain available.

Public-page fetching is credential-free and bounded, with public-address and redirect checks. Search snippets are candidates, never citeable evidence. Final answers validate the reference ID, final URL, title, read time, content hash and continuous actual quote against a saved page snapshot. Snapshots remain private to their turn/session and expire after 24 hours. Untrusted page text cannot grant tool permissions or change the chosen model. Web references do not rewrite published articles.

Send `X-VidLens-Contract: 2` on HTTP requests. Returned SSE URLs retain `contract=2`; old clients receive `CLIENT_UPDATE_REQUIRED` before consuming external citations and must stop reconnection until upgraded. This does not change published article schema/content.

Actual provider availability and interpretation quality require separate real endpoint checks. Fixture tests do not prove billing, ongoing availability or long-video quality.
