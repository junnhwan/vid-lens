# README visual assets

The Chinese [README](../README.md) and [English README](../README.en.md) share the assets in [`docs/images/`](images/). The seven PNG screenshots below were supplied by the user from real desktop tests and copied without edits. Progress counters and elapsed times reflect the page state in each capture; they are not a service guarantee. No extra video processing or model calls were started to create the images.

## Screenshots

| File | Application view | Contents |
| --- | --- | --- |
| `readme-demo-home.png` | `001.png` | Home page, link submission, and model/depth selectors |
| `readme-process-plan.png` | `013.png` | Current stage, execution plan, and overview frames |
| `readme-process-source-detail.png` | `014.png` | Subtitle check, video acquisition, and overview-frame records |
| `readme-process-writing-progress.png` | `015.png` | Article drafting with 6 of 12 planned chapters saved |
| `readme-article-detail.png` | `017.png` | Article text, timestamp citations, and an original video frame |
| `readme-article-map.png` | `019.png` | Generated article page with its chapter content map |
| `readme-frame-qa.png` | `021.png` | Selected passage, related video frame, and sourced Q&A |

The six processing, article, and Q&A captures use a 10:08 YouTube video titled *RAG Explained For Beginners*. UI language, model label, counters, and elapsed time are observations from the supplied captures, not default recommendations, benchmarks, or service guarantees. These static screenshots do not make the test article an interactive public example; ordinary visitor articles and Q&A remain private to their anonymous session.

Only explicitly prepared public examples appear in the screenshots. Ordinary visitor articles and Q&A remain private to their anonymous session. When refreshing images, check that no credentials, private service settings, browser sessions, private requests, or personal data appear in the capture.

Earlier `.jpg` captures and the superseded PNGs `readme-process-source.png`, `readme-process-writing.png`, `readme-process-review.png`, and `readme-demo-article.png` remain in `docs/images/` but are not used by the current README. If they are reused, retain their original source links and attribution.

## Banner

The Chinese and English READMEs select between `readme-banner-light.svg` and `readme-banner-dark.svg` with a `<picture>` element, so the background follows the reader's color scheme. Both are self-contained 1440 × 300 SVGs, with a flat background, generous whitespace, the project's existing favicon and vector wordmark on the left, and two lines of copy on the right:

> Your video, made readable.<br>
> **Read. Revisit. Ask.**

The centered README header also includes the project title, a localized short tagline, current technology badges, language links, and demo/local-setup links.

The logo is derived from [`frontend/public/favicon.svg`](../frontend/public/favicon.svg), and the wordmark paths from `Wordmark` in [`SiteHeader.tsx`](../frontend/src/ui/SiteHeader.tsx). The SVGs have no scripts or external resources. Only the two-line copy uses a system monospace font stack; the logo and wordmark are paths.

The header is brand identity, not a product screenshot.

## Architecture

`readme-architecture.svg` is editable, self-contained SVG with no scripts, external fonts, or external images. It uses English labels so both READMEs can share the same diagram. Its text description and each README provide an accessible alternative.

The diagram summarizes the current single-process Go API and worker pools, Eino evidence tools, PostgreSQL control state and Outbox, Redis / Asynq delivery and resource coordination, and persistent application files. It does not introduce separate microservices or enabled vector retrieval.

Check these source areas when changing the architecture:

- [`cmd/vidlens`](../cmd/vidlens/) — process composition.
- [`internal/media`](../internal/media/) — source acquisition and owned frames.
- [`internal/investigator`](../internal/investigator/) — Eino model and evidence tools.
- [`internal/jobs`](../internal/jobs/) and [`internal/store`](../internal/store/) — delivery, state, checkpoints, ownership, and publication.
- [Execution and recovery](backend/execution.md) and [model selection and article exports](backend/product-features.md) — implementation contracts.
