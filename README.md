# VidLens

VidLens turns a Bilibili or YouTube video into a read-only illustrated article and answers questions about the same video. Visitors use an anonymous session. The desktop Web application uses React, TypeScript and Vite; the backend uses Go and Eino. PostgreSQL owns requests, evidence and publication. Redis/Asynq delivers work, caches small indices and coordinates resource admission.

## Layout

- `cmd/vidlens/`: backend entrypoint.
- `internal/`: HTTP API, investigation, media, jobs, storage and resource controls.
- `frontend/`: the application frontend.
- `docs/backend/`: setup, integration and execution documentation.
- `docs/site/`: public documentation rendered at `/docs`.
- `docs/prototype/`: a separate static design reference; it does not call the API.
- `scripts/`: development and verification tools.

The root `go.mod` defines one backend module. Agent instructions, session context, private plans, credentials and runtime artifacts are excluded from Git.

## Start

Requires Go 1.26+, PostgreSQL 17, Redis 7, FFmpeg/ffprobe and `yt-dlp[default]==2026.8.19` (including its matching EJS scripts). YouTube also needs Deno 2.3+ or Node 22+; the backend enables Node alongside yt-dlp's default Deno runtime. The Docker image includes Deno. Bilibili and YouTube sources are acquired anonymously and never use the owner's platform Cookie file or browser login. Bilibili uses the public web API over a direct connection and selects the highest resolution actually returned, within the configured resource limits; authenticated-only qualities are unavailable. The model must support images and streaming tool calls. ASR uses a synchronous OpenAI-compatible multipart transcription endpoint. The backend currently uses Unix system calls; use Linux/Docker on Windows.

```sh
docker compose -p vidlens-dev -f compose.backend.yaml up -d postgres redis
mkdir -p .local
cp config.example.yaml .local/backend.yaml
chmod 600 .local/backend.yaml
# Fill model/ASR settings, independent random secrets and an absolute data_dir.
go run ./cmd/vidlens --config .local/backend.yaml
```

In another terminal:

```sh
cd frontend
npm ci
npm run dev -- --host 127.0.0.1 --port 5173 --strictPort
```

Open `http://127.0.0.1:5173`. The Vite development proxy targets backend port 8086 by default. See [startup and owner configuration](docs/backend/startup.md), [frontend integration](docs/backend/frontend-integration.md) and [execution and recovery](docs/backend/execution.md).

For a containerized backend, fill the ignored configuration first, then use `docker compose -p vidlens-dev -f compose.backend.yaml --profile app up -d --build`. The Compose file supplies default development database credentials; change them for other environments and keep infrastructure isolated from existing stacks.

## Verify

```sh
go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build -o .local/bin/vidlens ./cmd/vidlens

cd frontend
npm test
npm run build
```

The frontend build includes TypeScript checking. Backend integration tests additionally use `VIDLENS_TEST_DATABASE_URL` and `VIDLENS_TEST_REDIS_URL`; point them only at an isolated test database and Redis logical DB. PostgreSQL tests create random schemas and remove only those schemas. Media tests require FFmpeg/ffprobe.

Production requires a same-origin static server/API proxy with unbuffered SSE, private configuration and persistent data. This branch has no GitHub Actions deployment workflow; pushing code alone does not deploy it.

See [models, site features and private outputs](docs/backend/product-features.md) for owner password setup, frozen model selection, exports and optional free web references. [The nginx example](config-examples/nginx.conf.example) supports SPA deep links without swallowing API/static-asset 404s. Browser `?mock=1` previews exist only in development, carry an explicit preview label and are disabled in production builds.
