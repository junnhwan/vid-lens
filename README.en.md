<div align="center">

# VidLens · 映知

Understand long videos by questioning them — and turn them into notes.

[简体中文](README.md) · **English**

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React + Vite](https://img.shields.io/badge/React%20%2B%20Vite-SPA-315e48?logo=react)](https://vite.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL%20%2B%20pgvector-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org)

</div>

VidLens is a long-video understanding and learning platform built with Go and Vite + React. Upload a video or import a URL, and the system asynchronously produces three kinds of evidence — speech transcripts, on-screen OCR, and visual descriptions — and indexes them. Ask questions directly, or let the agent retrieve passages, extract frames to verify what is on screen, and then shape the material into a versioned study note that it keeps revising across the conversation.

## Core Features

- **Chunked, resumable uploads**: Redis tracks chunk state and MinIO merges server-side, so an interrupted large file only needs the missing chunks; an MD5 match against a finished object is reused directly.
- **RabbitMQ orchestration**: transcription, summary generation, visual analysis, and index building are scheduled as stages, with manual acknowledgments, retry backoff, leases, and idempotency, reusing completed stages and ASR segments.
- **Layered summary reduction**: long audio is split into overlapping windows, transcribed with bounded concurrency, then aligned and deduplicated. When the context limit is reached, segment summaries merge layer by layer with persisted intermediates, so a failure reuses finished segments instead of redoing the whole video.
- **Hybrid retrieval**: pgvector similarity runs alongside BM25 implemented in Go; queries are rewritten, the result sets fused with RRF, and the merged list reranked to select the passages behind an answer and its citations.
- **Agent tool loop**: following a ReAct shape, the agent plans each next step, calls transcript search, neighboring transcript windows, and visual-evidence tools, and converges into a cited answer — any citation to something no tool actually observed is rejected.
- **On-demand frame extraction**: frames are pulled inside an already-located time window and handed to a vision model to fill in visual evidence. Results are cached per frame and per investigation goal, so a repeat investigation reuses them instead of calling the model again.
- **Study notes revised by an agent**: material becomes a structured note that the agent revises with block-level patches for corrections, additions, and restructuring. A transactional outbox, operation idempotency, and checkpoint recovery keep a crashed process from producing duplicate note versions. Summaries likewise take anchored renames and terminology fixes without regenerating the full text.
- **Execution budgets and recovery**: each AI profile configures call, time, token, and visual budgets with capacity reserved for the final answer. Execution records persist, so an interrupted stream can be queried for status and saved results without automatically re-running anything.
- **Cross-session preferences**: revocable response preferences (language, verbosity, format) are stored only after explicit consent, recognized from explicit statements in the conversation, written through an outbox, and injected into later sessions. Disabled by default; enable it in configuration.
- **Request governance and rate limiting**: per-user ASR, LLM, Embedding, and Vision configuration with API keys sealed in AES-256-GCM. A single Redis + Lua script atomically checks rate ceilings across user, operation, provider, and model, and reported usage distinguishes measured from estimated.
- **Data boundaries and observability**: PostgreSQL holds application state and execution records, pgvector is a rebuildable retrieval projection, and MinIO stores media. Retrieval, tool execution, and answer publication all enforce user ownership and knowledge-base membership. Structured logs, Prometheus metrics, and Grafana dashboards sit alongside `cmd/rag-eval`, `cmd/rag-audit`, and `cmd/rag-reindex`.

## Architecture

![VidLens · System architecture](docs/images/readme-architecture.en.svg)

## Screenshots

**Agent planning and execution: each step states why it is planning that way, calls a tool, and can be expanded to show the call details.**

![Agent planning and tool calls](docs/images/readme-agent-plan.png)

**Answers and citations: each citation keeps its time range and evidence modality, and can replay the matching passage.**

![Timestamped citations and replay](docs/images/readme-chat-citation.png)

**Knowledge canvas: nodes organize the concepts, and the side panel lists the evidence behind each conclusion with a way back to the video.**

![Study note canvas with linked evidence](docs/images/readme-artifact-canvas.png)

<details>
<summary>Execution cost and answer shape</summary>

**Run details: elapsed time, tool calls against the ceiling, model and retrieval calls, input / output tokens, and per-step timings.**

![Agent run details and budget usage](docs/images/readme-agent-budget.png)

**Deep analysis answer: organized by point, with conclusions drawn from retrieved transcript evidence.**

![Deep analysis answer](docs/images/readme-agent-answer.png)

</details>

<details>
<summary>Video details and multimodal evidence</summary>

**Transcript timeline: colored by speech transcript, on-screen OCR, and visual description, and clicking a block jumps to that moment.**

![Transcript timeline in the video view](docs/images/readme-video-transcript.png)

**Visual evidence: keyframes with their visual descriptions and on-screen text.**

![Visual evidence in the video view](docs/images/readme-video-visual.png)

**Tasks: video processing advances through ingestion, transcription, visual analysis, and indexing, with note generation listed alongside.**

![Processing task stages](docs/images/readme-tasks.png)

</details>

<details>
<summary>Study notes, mind map, and canvas</summary>

**Study note: chaptered body text, where each section can show its evidence, be questioned, or be revised by the agent; the page carries its own needs-review and sampling notices.**

![Study note body and evidence entries](docs/images/readme-artifact-note.png)

**Mind map: the same note body rendered as a map, not a second document.**

![Study note mind map view](docs/images/readme-artifact-map.png)

</details>

<details>
<summary>Dashboard, Q&A scopes, and settings</summary>

**Dashboard: resume the video you were learning, with counts for material, saved conversations, and pending work.**

![Dashboard](docs/images/readme-dashboard.png)

**Dashboard: recent conversations and recent study outputs.**

![Recent conversations and outputs](docs/images/readme-dashboard-recent.png)

**Q&A scopes: three entries — the whole video library, one knowledge base, or a single video.**

![Q&A scope picker](docs/images/readme-chat-scope.png)

**Knowledge bases: narrow questions and retrieval to a topic.**

![Knowledge-base list](docs/images/readme-knowledge-bases.png)

**Settings: appearance themes, and the dialogue, vision, speech, embedding, and rerank models inside one AI profile.**

![AI service settings](docs/images/readme-settings-ai.png)

</details>

## Stack and Quick Start

**Go · Gin · GORM · PostgreSQL / pgvector · Redis · RabbitMQ · MinIO · FFmpeg / yt-dlp · Vite + React**

Install Go 1.24+, Node.js 20+, Docker Compose, FFmpeg, and yt-dlp. Copy `.env.example` to `.env` and configure your connections and credentials. After signing in, configure models under **Settings → AI Services**. When the OCR binary is missing, the visual OCR stage is skipped automatically and transcripts and visual descriptions remain available.

```bash
# From the repository root: start dependencies and the backend
cp .env.example .env   # First-time setup only; skip if .env already exists
# Set independent stable VIDLENS_JWT_SECRET / VIDLENS_API_KEY_SECRET in .env
docker compose up -d
go run ./cmd/server
```

Start the frontend in another terminal:

```bash
cd frontend
npm ci
npm run dev -- -p 5173
```

Open `http://127.0.0.1:5173`. The backend health endpoint is `http://127.0.0.1:8080/healthz`. On Windows, once the environment is configured, you can also use `make start` and `make status`; the startup script checks the local data directory.

## Engineering Documentation

[Architecture](docs/architecture/overview.md) · [Retrieval](docs/architecture/retrieval.md) · [Execution and recovery](docs/architecture/agent-streaming-contract.md) · [Evidence and citations](docs/architecture/agent-evidence.md) · [Note editing contract](docs/architecture/artifact-editing-contract.md) · [Note generation contract](docs/architecture/study-generation-v3-contract.md) · [Media understanding pipeline](docs/architecture/media-understanding-pipeline.md) · [Reliability and idempotency](docs/architecture/reliability.md) · [Preference memory](docs/architecture/agent-memory.md) · [Feedback and product regression](docs/eval/product-feedback.md) · [Documentation index](docs/README.md)

Engineering documentation is primarily in Chinese.
