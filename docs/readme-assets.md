# README visual assets

The Chinese [README](../README.md) and [English README](../README.en.md) share the assets in [`docs/images/`](images/). Screenshots were captured on October 16, 2026 from the desktop application, using the saved public examples bundled with the frontend. These captures predate the concise/standard/detailed selector. No new video processing, transcription, or generation was started for these captures.

## Screenshots

| File | Application view | Contents |
| --- | --- | --- |
| `readme-home.jpg` | `/` | Actual home page, model selector, optional focus entry, and the serving host's disk indicator |
| `readme-article.jpg` | `/example?video=workflow` | Published article, content map, and section navigation |
| `readme-evidence.jpg` | `/example?video=workflow` | Grill section, original video frame, caption, and timestamps |
| `readme-qa.jpg` | `/example?video=skills&view=qa&question=frame` | Saved frame question, selected PR image, and start of the answer |
| `readme-process.jpg` | `/example?video=skills&view=process` | Completed processing replay with original public steps and evidence frames |

Screenshots preserve the rendered application. UI and saved content are in Chinese. The model labels, counters, disk capacity, and elapsed times are observations from that capture or saved run; they are not default model recommendations, performance benchmarks, or current service guarantees. Replay timing compresses waits. The workflow example has no saved intermediate Q&A steps; the UI retains that disclosure. The Skills example has saved intermediate steps. Do not synthesize missing steps or relabel a replay as a live run.

Only explicitly prepared public examples appear in the screenshots. Ordinary visitor articles and Q&A remain private to their anonymous session. When refreshing images, check that no credentials, private service settings, browser sessions, private requests, or personal data appear in the capture.

## Video attribution

The screenshots contain frames from videos featuring Matt Pocock and their generated commentary. Video frames remain attributable to their original creators and are used here to demonstrate the application's handling of evidence; they are not project-owned illustrations.

- Workflow example: [Bilibili source](https://www.bilibili.com/video/BV14Upq6yEq7?p=1), as recorded in [`workflow.json`](../frontend/public/example/workflow.json).
- Skills v1.3 example: [YouTube source](https://www.youtube.com/watch?v=BsJGo1wFTvQ), as recorded in [`skills.json`](../frontend/public/example/skills.json).

Keep the original source links, timestamps, and visible attribution when updating these demonstrations.

## Banner

The header uses two self-contained SVG files: `readme-banner-light.svg` and `readme-banner-dark.svg`. Both are 1440 × 300, with a flat background, generous whitespace, the project's existing favicon and vector wordmark on the left, and two lines of copy on the right:

> Your video, made readable.<br>
> **Read. Revisit. Ask.**

A `<picture>` element selects the matching GitHub light or dark theme; the light version is the fallback. The language links and badges follow directly below it, without repeating the project name and tagline.

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
