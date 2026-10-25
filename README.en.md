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

[Open the real examples](https://vidlens.wanjune.qzz.io/example). The page shows published articles selected for public display. Browsing them makes no new model calls.

## Reading and asking questions

These screenshots come from an actual interpretation of 3Blue1Brown's [But what is a neural network? | Deep learning chapter 1](https://www.youtube.com/watch?v=aircAruvnKk) (18:39), showing the desktop Web flow from a video link to an illustrated article and questions about its frames.

### Submit a video and choose how to read it

![VidLens home page: paste a video link, choose a model and interpretation depth, add a focus, or open a recent article.](docs/images/readme-demo-home.png)

### See the whole video in a content map

![VidLens article opening: chapter navigation and the full content map, with controls to share, export, revisit processing, or ask about the video.](docs/images/readme-article-map.png)

### Read the article alongside real video frames

![VidLens article body: chapter navigation, timestamped explanations, and an original neural-network frame with a follow-up question link.](docs/images/readme-article-detail.png)

### Ask as you read, with sources in the answer

![VidLens side-by-side Q&A: keep reading on the left while the answer explains weight colors and horizontal edge detection with timestamped sources and a video frame.](docs/images/readme-frame-qa.png)

<details>
<summary>Show passage questions, frame investigation, and focused Q&A (2 screenshots)</summary>

#### Select a passage and follow the investigation

![VidLens passage and frame Q&A: the selected passage remains visible while the sidebar shows video frames being inspected and their timestamps.](docs/images/readme-qa-investigation.png)

#### Switch to the focused Q&A view

![VidLens focused Q&A: a wider question area with an expanded answer history showing additional video frames gathered for the question.](docs/images/readme-qa-focus.png)

</details>

<details>
<summary>Show processing from source preparation to publication (5 screenshots)</summary>

Progress, call counts, and elapsed times reflect the page state at capture; they are not a processing-time guarantee.

#### Follow the current stage and execution plan

![VidLens processing page: reading the video's text and frames, with the current stage and execution plan visible.](docs/images/readme-process-plan.png)

#### Prepare subtitles and overview frames

![VidLens source preparation: subtitles, the full video, and media checks are complete, with 12 overview frames spanning the video.](docs/images/readme-process-source-detail.png)

#### Organize and check the article structure

![VidLens writing progress: the text has been read and frames checked; the Agent is organizing an outline covering the full video.](docs/images/readme-process-writing-progress.png)

#### Check frames, save observations, and write the article

![VidLens execution history: inspect two neural-network frames, save visual observations and the outline, then save the article and validate its citations.](docs/images/readme-process-evidence.png)

#### Open the finished article

![VidLens article ready: all 10 chapters are saved, with preparation, reading, frame inspection, writing, review, and publication complete.](docs/images/readme-process-complete.png)

</details>

## Core features

- **Start with a video**: Supports a Bilibili video or individual part, and a single YouTube video. Uses available subtitles first, with speech transcription when they are missing. Personal platform cookies are not required.
- **Read with sources**: Articles include chapters, real video frames, timestamped citations, and a content map. Export as Markdown or an illustrated ZIP.
- **Get the article first**: Read and export as soon as it is generated. One automatic review offers separate advice; request another review when needed.
- **Ask about evidence**: The Agent searches the video's text and frames for support. Ask about the article, a passage, or a selected frame; answers identify sources and unresolved points.
- **Follow the work**: See source preparation, investigation, writing, and review progress, then revisit saved steps. When enabled, public webpages can supplement an individual question.
- **Share a single record**: Create a revocable read-only link to its article and processing history. Private Q&A stays private.

## Architecture

This diagram shows how a video link becomes an illustrated article and how the current service, Agent, queue, and persistent storage fit together.

![VidLens architecture: video preparation, Agent evidence investigation, article publication, and current service components.](docs/images/readme-architecture.svg)

## Videos that work well

Technical explainers, science videos, courses, and tutorials with clear narration are a good fit. Music, scenery, and montages may provide less information to organize; still frames also cannot fully convey continuous action, rhythm, or story. Unrelated videos, and very large videos submitted to abuse the service, are removed. Results depend on subtitles, transcription, frame clarity, and model availability. See the [video selection guide](docs/site/start.md).

## Local development

Requires Go 1.26+, Node.js 22+, PostgreSQL 17, Redis 7, and FFmpeg. The backend needs a Linux/Docker environment on Windows. See [startup and configuration](docs/backend/startup.md) for details.

## Documentation

[User guide](docs/site/index.md) · [Privacy and retention](docs/site/privacy.md) · [Agent execution and recovery](docs/backend/execution.md)

## Community and feedback

VidLens announcements and discussions are on [Linux.do](https://linux.do/). Share your experience, ask questions, and discuss ideas there. Technical questions and reproducible bugs can also go to [GitHub Issues](https://github.com/junnhwan/vid-lens/issues).

The site is designed for desktop browsers and does not require registration. Articles and Q&A belong to the current anonymous session. Explicitly sharing a single record grants read-only access to its article and processing history; private Q&A is excluded. See [privacy and retention](docs/site/privacy.md).

## Star History

[![VidLens Star History](https://api.star-history.com/chart?repos=junnhwan/vid-lens&type=date&legend=top-left)](https://www.star-history.com/?repos=junnhwan%2Fvid-lens&type=date&legend=top-left)
