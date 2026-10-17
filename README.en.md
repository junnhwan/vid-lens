<div align="center">

# VidLens

**Turn videos into readable, revisitable, question-ready articles.**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./docs/images/readme-banner-dark.svg" />
  <source media="(prefers-color-scheme: light)" srcset="./docs/images/readme-banner-light.svg" />
  <img alt="VidLens — Your video, made readable. Read. Revisit. Ask." src="./docs/images/readme-banner-light.svg" width="1440" />
</picture>

[简体中文](README.md) · **English**

[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev) [![React 18 · Vite 5](https://img.shields.io/badge/React-18%20%2B%20Vite%205-61DAFB?logo=react&logoColor=black)](https://vite.dev) [![PostgreSQL 17](https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org) [![Redis 7 · Asynq](https://img.shields.io/badge/Redis-7%20%2B%20Asynq-DC382D?logo=redis&logoColor=white)](https://github.com/hibiken/asynq) [![FFmpeg](https://img.shields.io/badge/FFmpeg-media%20processing-007808?logo=ffmpeg&logoColor=white)](https://ffmpeg.org)

[Live demo](https://vidlens.wanjune.qzz.io/) · [Real examples](https://vidlens.wanjune.qzz.io/example) · [Linux.do community](https://linux.do/) · [Local setup](docs/backend/startup.md)

</div>

VidLens turns a Bilibili or YouTube video into a desktop reading experience: an illustrated article with chapters, real video frames, timestamped sources, and a content map. Choose an interpretation depth and focus, revisit the sources, and ask follow-up questions as you read.

## Try a real example

| Example | What you can explore |
| --- | --- |
| [From Grill to Retro · Bilibili / 09:04](https://vidlens.wanjune.qzz.io/example?video=workflow) | Illustrated article, content map, processing replay, and Q&A |
| [Skills v1.3: Three New Skills · YouTube / 14:34](https://vidlens.wanjune.qzz.io/example?video=skills) | Evidence gathering, timestamped citations, and questions about a frame |

These are saved real results. Browsing them makes no new model calls.

## Processing and reading

The processing, article, and Q&A screenshots below come from a real test of a 10:08 YouTube video. Progress and elapsed times show the page state at capture; they are not a processing-time guarantee.

### Submit a video

![VidLens home page: paste a video link and choose a model and interpretation depth.](docs/images/readme-demo-home.png)

### Follow the current stage and plan

![VidLens processing page: the current stage, execution plan, and overview frames from across the video.](docs/images/readme-process-plan.png)

### Prepare source text and visual evidence

![VidLens source preparation log: subtitle checks, video acquisition, and overview frames spanning the video.](docs/images/readme-process-source-detail.png)

### Draft the article in batches

![VidLens processing progress: 6 of 12 planned chapters are saved while the Agent continues writing.](docs/images/readme-process-writing-progress.png)

### Read the article with timestamped sources

![VidLens illustrated article: body text with timestamped citations and a real video frame.](docs/images/readme-article-detail.png)

### Browse the content map

![Generated VidLens article page with a chapter content map and links to export, revisit processing, and ask about the video.](docs/images/readme-article-map.png)

### Ask about a passage or frame

![VidLens video Q&A: select article content and view an answer with timestamped sources alongside the related video frame.](docs/images/readme-frame-qa.png)

## Core features

- **Start with a video**: Supports a Bilibili video or individual part, and a single YouTube video. Uses available subtitles first, with speech transcription when they are missing. Personal platform cookies are not required.
- **Read with sources**: Articles include chapters, real video frames, timestamped citations, and a content map. Export as Markdown or an illustrated ZIP.
- **Ask about evidence**: The Agent searches the video's text and frames for support. Ask about the article, a passage, or a selected frame; answers identify sources and unresolved points.
- **Follow the work**: See source preparation, investigation, writing, and review progress, then revisit saved steps. When enabled, public webpages can supplement an individual question.

## Architecture

This diagram shows how a video link becomes an illustrated article and how the current service, Agent, queue, and persistent storage fit together.

![VidLens architecture: video preparation, Agent evidence investigation, article publication, and current service components.](docs/images/readme-architecture.svg)

## Videos that work well

Technical explainers, science videos, courses, and tutorials with clear narration are a good fit. Music, scenery, and montages may provide less information to organize; still frames also cannot fully convey continuous action, rhythm, or story. Results depend on subtitles, transcription, frame clarity, and model availability. See the [video selection guide](docs/site/start.md).

## Local development

Requires Go 1.26+, Node.js 22+, PostgreSQL 17, Redis 7, and FFmpeg. The backend needs a Linux/Docker environment on Windows. See [startup and configuration](docs/backend/startup.md) for details.

## Documentation

[User guide](docs/site/index.md) · [Privacy and retention](docs/site/privacy.md) · [Agent execution and recovery](docs/backend/execution.md)

## Community and feedback

VidLens announcements and discussions are on [Linux.do](https://linux.do/). Share your experience, ask questions, and discuss ideas there. Technical questions and reproducible bugs can also go to [GitHub Issues](https://github.com/junnhwan/vid-lens/issues).

The site is designed for desktop browsers and does not require registration. Articles and Q&A belong to the current anonymous session. See [privacy and retention](docs/site/privacy.md).

## Star History

[![VidLens Star History](https://api.star-history.com/chart?repos=junnhwan/vid-lens&type=date&legend=top-left)](https://www.star-history.com/?repos=junnhwan%2Fvid-lens&type=date&legend=top-left)
