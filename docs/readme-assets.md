# README visual assets

The Chinese [README](../README.md) and [English README](../README.en.md) share the assets in [`docs/images/`](images/). The eleven PNG screenshots below were supplied by the user on 2026-10-18 from a real desktop test and copied without edits at their original 3072 × 1600 resolution. The README shows the home page, content map, article, and Q&A answer first; expandable sections contain the additional Q&A views and processing history.

## Screenshots

| File | Supplied capture | Contents |
| --- | --- | --- |
| `readme-demo-home.png` | `011.png` | Updated home page, link submission, model/depth selectors, and recent articles |
| `readme-article-map.png` | `006.png` | Article opening, chapter navigation, full content map, and export controls |
| `readme-article-detail.png` | `007.png` | Article text, timestamp citations, and an original neural-network frame |
| `readme-frame-qa.png` | `009.png` | Side-by-side reading and an answer with timestamped sources and a video frame |
| `readme-qa-investigation.png` | `008.png` | Passage selection and frame investigation in the Q&A sidebar |
| `readme-qa-focus.png` | `010.png` | Focused Q&A with expanded frame-gathering history |
| `readme-process-plan.png` | `001.png` | Current stage and execution plan while reading the video |
| `readme-process-source-detail.png` | `002.png` | Subtitle checks, video acquisition, and 12 overview frames |
| `readme-process-writing-progress.png` | `003.png` | Organizing the article outline after reading text and checking frames |
| `readme-process-evidence.png` | `004.png` | Detailed frame checks, saved observations, article writing, and citation validation |
| `readme-process-complete.png` | `005.png` | Completed processing with all 10 chapters saved and the article ready to open |

The source video is 3Blue1Brown's [But what is a neural network? | Deep learning chapter 1](https://www.youtube.com/watch?v=aircAruvnKk), shown as 18:39 in the application. The original video imagery belongs to its creator. UI language, model label, counters, and elapsed time are observations from the supplied captures, not default recommendations, benchmarks, or service guarantees.

The user supplied these screenshots for public README display. Static screenshots do not make the test article an interactive public example; ordinary visitor articles and Q&A remain private to their anonymous session. Future captures should likewise be selected for publication and checked for credentials, private service settings, session tokens, and personal data.

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
