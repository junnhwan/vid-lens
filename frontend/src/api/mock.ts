import type {
  ActivityStep,
  Answer,
  Article,
  ArticleResponse,
  Conversation,
  Failure,
  Frame,
  QuestionContext,
  RequestView,
  SourceSummary,
  TurnView,
} from "../contract/types"
import { quoteMatchesBlock } from "../contract/text"
import { ApiError } from "./error"
import type { LiveSocket } from "./live"
import type { Runtime } from "./runtime"

const now = () => new Date().toISOString()
const closed = "/frames/closed.jpg"
const opened = "/frames/open.jpg"

function frame(id: string, time: number, caption: string, file: string): Frame {
  return {
    id,
    snapshot_id: "snap_mock",
    time_ms: time,
    requested_time_ms: time,
    width: 640,
    height: 360,
    mime_type: "image/jpeg",
    caption,
    crop: null,
    replaces_frame_id: null,
    availability: "ready",
    unavailable_reason: null,
    expires_at: null,
    urls: { thumbnail: file, full: file },
  }
}

const frameClosed = frame("frm_closed", 312_000, "展开前的编辑界面", closed)
const frameOpen = frame("frm_open", 324_000, "展开后的菜单", opened)
const frames = [frameClosed, frameOpen]
const developmentModel = {
  id: "development",
  revision_id: "development",
  display_name: "开发演示模型",
  brand_id: "generic",
  selection_locked: true,
}

function step(
  partial: Partial<ActivityStep> & Pick<ActivityStep, "id" | "title" | "status" | "first_seq" | "last_seq" | "segment_no" | "run_id">,
): ActivityStep {
  return {
    phase: "investigate",
    purpose: null,
    source_range: null,
    frame_ids: [],
    frames: [],
    outcome: null,
    retry_of_step_id: null,
    started_at: now(),
    finished_at: null,
    ...partial,
  }
}

const article: Article = {
  schema_version: 1,
  id: "art_mock",
  snapshot_id: "snap_mock",
  source: {
    source_id: "src_mock",
    snapshot_id: "snap_mock",
    platform: "bilibili",
    canonical_url: "https://www.bilibili.com/video/BV1mockvideo?p=1",
    title: "把重复操作变成可复用流程",
    duration_ms: 2_142_000,
    part: { key: "1", label: "第 1 P" },
    text_provenance: {
      origin: "platform_subtitles",
      coverage: "complete",
      language: "zh",
    },
  },
  title: "把重复操作变成可复用流程",
  published_at: now(),
  sections: [
    {
      id: "scene",
      title: "适用场景",
      blocks: [
        {
          id: "blk_scene",
          type: "paragraph",
          origin: "video",
          content: [
            {
              type: "text",
              text: "日常里有些操作会反复出现。视频把这件事收成一条主线：与其每次重做，不如把步骤存成一套可以再次运行的流程。",
              marks: [],
            },
          ],
        },
      ],
    },
    {
      id: "steps",
      title: "配置步骤",
      blocks: [
        {
          id: "blk_steps",
          type: "paragraph",
          origin: "video",
          content: [
            { type: "text", text: "配置从菜单开始。", marks: [] },
            { type: "citation", citation_id: "cite_closed" },
            { type: "text", text: " 时界面还停在编辑状态。到 ", marks: [] },
            { type: "citation", citation_id: "cite_open" },
            {
              type: "text",
              text: "，菜单展开，能看到添加步骤、保存流程和运行。",
              marks: [],
            },
          ],
        },
        {
          id: "blk_menu",
          type: "figure",
          origin: "video",
          frame_ids: ["frm_open"],
          layout: "single",
          caption: "展开菜单，查看可添加的步骤。",
          citation_ids: ["cite_open"],
        },
        {
          id: "blk_why",
          type: "paragraph",
          origin: "background_explanation",
          content: [
            {
              type: "text",
              text: "先保存，是为了让当前步骤成为一份可复用的配置。原视频没有单独演示失败画面，这段只补充理解顺序所需要的原因。",
              marks: [],
            },
          ],
        },
      ],
    },
    {
      id: "cautions",
      title: "注意事项",
      blocks: [
        {
          id: "blk_caution",
          type: "paragraph",
          origin: "video",
          content: [
            {
              type: "text",
              text: "菜单里的名称以画面为准：保存流程不等于已经运行。已查看的画面没有看到运行完成后的结果页。",
              marks: [],
            },
          ],
        },
      ],
    },
  ],
  mind_map: {
    root_id: "map_root",
    nodes: [
      {
        id: "map_root",
        parent_id: null,
        label: "可复用流程",
        section_id: null,
      },
      {
        id: "map_scene",
        parent_id: "map_root",
        label: "适用场景",
        section_id: "scene",
      },
      {
        id: "map_steps",
        parent_id: "map_root",
        label: "配置步骤",
        section_id: "steps",
      },
      {
        id: "map_cautions",
        parent_id: "map_root",
        label: "注意事项",
        section_id: "cautions",
      },
      {
        id: "map_save",
        parent_id: "map_steps",
        label: "保存流程",
        section_id: "steps",
      },
      {
        id: "map_check",
        parent_id: "map_steps",
        label: "对照画面核对",
        section_id: "steps",
      },
      {
        id: "map_unseen",
        parent_id: "map_check",
        label: "结果页尚未出现",
        section_id: "cautions",
      },
    ],
  },
  citations: {
    cite_closed: {
      id: "cite_closed",
      kind: "video_frame",
      label: "05:12 画面",
      snapshot_id: "snap_mock",
      frame_id: "frm_closed",
      time_ms: 312_000,
    },
    cite_open: {
      id: "cite_open",
      kind: "video_frame",
      label: "05:24 画面",
      snapshot_id: "snap_mock",
      frame_id: "frm_open",
      time_ms: 324_000,
    },
  },
  limitations: [
    {
      code: "not_observed",
      message: "已查看的画面未覆盖运行完成后的结果页。",
      section_ids: ["cautions"],
      citation_ids: ["cite_open"],
    },
  ],
  quality: "with_limitations",
}

interface Job {
  id: string
  revision: number
  inputUrl: string
  status: string
  reuse: string
  retryOf: string | null
  articleId: string | null
  source: SourceSummary | null
  runId: string | null
  runKind: string
  runStatus: string
  phase: string | null
  wait: string | null
  seq: number
  steps: ActivityStep[]
  failure: Failure | null
  stage: number
  createdAt: string
  updatedAt: string
  timer: ReturnType<typeof setTimeout> | null
}

interface Talk {
  id: string
  conversationId: string
  requestId: string
  turnNo: number
  revision: number
  status: string
  question: string
  context: QuestionContext
  retryOf: string | null
  answer: Answer | null
  failure: Failure | null
  runId: string
  runStatus: string
  phase: string | null
  wait: string | null
  seq: number
  steps: ActivityStep[]
  stage: number
  createdAt: string
  updatedAt: string
  timer: ReturnType<typeof setTimeout> | null
}

interface Recorded {
  body: string
  id: string
}

class MockSocket implements LiveSocket {
  readyState = 0
  onerror: ((this: LiveSocket, ev: Event) => void) | null = null
  private listeners = new Map<string, ((event: MessageEvent) => void)[]>()

  constructor(
    readonly url: string,
    private world: MockWorld,
  ) {
    queueMicrotask(() => {
      if (this.readyState === 2) return
      this.readyState = 1
      this.world.attach(this)
    })
  }

  addEventListener(type: string, listener: (event: MessageEvent) => void) {
    const list = this.listeners.get(type) ?? []
    list.push(listener)
    this.listeners.set(type, list)
  }

  emit(type: string, data: unknown) {
    if (this.readyState !== 1) return
    const event = {
      data: JSON.stringify(data),
      lastEventId: "",
    } as MessageEvent
    for (const listener of this.listeners.get(type) ?? []) listener(event)
  }

  close() {
    this.readyState = 2
    this.world.detach(this)
  }
}

function terminal(status: string) {
  return status === "completed" || status === "failed" || status === "canceled"
}

function sourceFor(url: string): SourceSummary {
  const platform = url.includes("youtube.com") || url.includes("youtu.be") ? "youtube" : "bilibili"
  return {
    source_id: "src_mock",
    snapshot_id: "snap_mock",
    platform,
    canonical_url: url.trim(),
    title: "把重复操作变成可复用流程",
    duration_ms: 2_142_000,
    part: platform === "bilibili" ? { key: "1", label: "第 1 P" } : null,
    text_provenance: {
      origin: "platform_subtitles",
      coverage: "complete",
      language: "zh",
    },
  }
}

export interface MockRuntime extends Runtime {
  advance(): void
  dispose(): void
}

export function createMockRuntime(options: { auto?: boolean; intervalMs?: number } = {}): MockRuntime {
  const world = new MockWorld(options.auto !== false, options.intervalMs ?? 700)
  return {
    api: world.api(),
    events: (url) => world.open(url),
    advance: () => world.advance(),
    dispose: () => world.dispose(),
  }
}

class MockWorld {
  private requests = new Map<string, Job>()
  private turns = new Map<string, Talk>()
  private conversations = new Map<string, Conversation>()
  private requestKeys = new Map<string, Recorded>()
  private turnKeys = new Map<string, Recorded>()
  private sockets = new Set<MockSocket>()
  private seq = 0

  constructor(
    private auto: boolean,
    private intervalMs: number,
  ) {}

  open(url: string) {
    return new MockSocket(url, this)
  }

  attach(socket: MockSocket) {
    this.sockets.add(socket)
    const parsed = new URL(socket.url, "http://vidlens.local")
    const target = this.target(parsed.pathname)
    if (!target) return
    const after = parsed.searchParams.get("after")
    const position = this.position(target)
    if (after === position.cursor && !terminal(this.statusOf(target))) return
    this.send(socket, target, "view.snapshot")
  }

  detach(socket: MockSocket) {
    this.sockets.delete(socket)
  }

  dispose() {
    for (const job of this.requests.values()) if (job.timer) clearTimeout(job.timer)
    for (const turn of this.turns.values()) if (turn.timer) clearTimeout(turn.timer)
    for (const socket of this.sockets) socket.close()
  }

  advance() {
    const turn = [...this.turns.values()].reverse().find((item) => !terminal(item.status))
    if (turn) {
      this.stepTurn(turn)
      return
    }
    const job = [...this.requests.values()].reverse().find((item) => !terminal(item.status))
    if (job) this.stepJob(job)
  }

  api(): Runtime["api"] {
    return {
      session: async () => ({
        schema_version: 1,
        session: { id: "ses_mock", expires_at: "2026-11-14T00:00:00Z" },
        csrf_token: "csrf_mock",
        capabilities: {
          supported_platforms: ["bilibili", "youtube"],
          accepting_requests: true,
          question_chars_max: 4000,
          selection_chars_max: 1000,
          idempotency_window_seconds: 86400,
        },
      }),
      createRequest: async (videoUrl, key) => this.createRequest(videoUrl, key),
      getRequest: async (id) => this.requestView(this.mustJob(id)),
      cancelRequest: async (id) => this.cancelJob(this.mustJob(id)),
      retryRequest: async (id, key) => this.retryJob(this.mustJob(id), key),
      article: async (id) => this.article(this.mustJob(id)),
      frame: async (_requestId, frameId) => {
        const found = frames.find((item) => item.id === frameId)
        if (!found)
          throw new ApiError({
            status: 404,
            code: "RESOURCE_NOT_FOUND",
            detail: "资源不存在。",
          })
        return found
      },
      activity: async (url) => {
        const target = this.target(new URL(url, "http://vidlens.local").pathname.replace(/\/activity$/, "/events"))
        const steps = target && "steps" in target ? target.steps : []
        return {
          schema_version: 1,
          items: steps,
          next_before: null,
          has_more: false,
          through_cursor: target ? this.position(target).cursor : "none",
          history_status: steps.length ? "available" : "none",
          history_expires_at: null,
        }
      },
      conversation: async (requestId) => this.conversation(this.mustJob(requestId)),
      turns: async (conversationId) => ({
        schema_version: 1,
        items: [...this.turns.values()]
          .filter((item) => item.conversationId === conversationId)
          .sort((a, b) => a.turnNo - b.turnNo)
          .map((item) => this.turnView(item)),
        next_before_turn_no: null,
        has_more: false,
      }),
      createTurn: async (conversationId, key, body) =>
        this.createTurn(
          conversationId,
          key,
          body as {
            question: string
            expected_last_turn_id: string | null
            context: QuestionContext
          },
        ),
      getTurn: async (_conversationId, turnId) => this.turnView(this.mustTurn(turnId)),
      cancelTurn: async (_conversationId, turnId) => this.cancelTurn(this.mustTurn(turnId)),
      retryTurn: async (conversationId, turnId, key, expected) => this.retryTurn(conversationId, this.mustTurn(turnId), key, expected),
    }
  }

  private createRequest(videoUrl: string, key: string) {
    const body = JSON.stringify({ video_url: videoUrl.trim() })
    const seen = this.requestKeys.get(key)
    if (seen) {
      if (seen.body !== body)
        throw new ApiError({
          status: 409,
          code: "IDEMPOTENCY_CONFLICT",
          detail: "同一次提交的内容不一致。",
        })
      return this.accepted(this.requestView(this.mustJob(seen.id)), true)
    }
    if (!videoUrl.trim())
      throw new ApiError({
        status: 400,
        code: "INVALID_ARGUMENT",
        detail: "先贴上链接。",
        meta: {
          field_errors: [{ field: "video_url", message: "先贴上链接。" }],
        },
      })
    const id = `req_${++this.seq}`
    const job: Job = {
      id,
      revision: 1,
      inputUrl: videoUrl.trim(),
      status: "waiting_source",
      reuse: "pending",
      retryOf: null,
      articleId: null,
      source: null,
      runId: `${id}-prepare`,
      runKind: "source.prepare",
      runStatus: "running",
      phase: "prepare_source",
      wait: null,
      seq: 1,
      steps: [
        step({
          id: `${id}-source`,
          title: "正在转写音频（4:00 / 35:42）",
          purpose: "平台没有可用字幕，按音频长度转写。",
          status: "running",
          first_seq: 1,
          last_seq: 1,
          segment_no: 1,
          run_id: `${id}-prepare`,
          phase: "prepare_source",
        }),
      ],
      failure: null,
      stage: 0,
      createdAt: now(),
      updatedAt: now(),
      timer: null,
    }
    this.requests.set(id, job)
    this.requestKeys.set(key, { body, id })
    this.kick(job)
    return this.accepted(this.requestView(job), false)
  }

  private stepJob(job: Job) {
    if (terminal(job.status)) return
    job.updatedAt = now()
    job.stage += 1
    if (job.stage === 1) {
      job.seq += 1
      job.steps[0] = {
        ...job.steps[0],
        title: "正在转写音频（18:00 / 35:42）",
        last_seq: job.seq,
      }
      this.publish(job, "activity.upsert")
      return
    }
    if (job.stage === 2) {
      job.steps[0] = {
        ...job.steps[0],
        status: "completed",
        finished_at: now(),
        outcome: { message: "已确认来源。", evidence_status: "not_applicable" },
        last_seq: job.seq,
      }
      job.status = "generating"
      job.reuse = "none"
      job.revision = 2
      job.source = sourceFor(job.inputUrl)
      job.runId = `${job.id}-generate`
      job.runKind = "article.generate"
      job.runStatus = "running"
      job.phase = "overview"
      job.seq = 1
      job.steps.push(
        step({
          id: `${job.id}-spine`,
          title: "正在梳理视频主线",
          purpose: "找出适合写成步骤的段落。",
          status: "running",
          first_seq: 1,
          last_seq: 1,
          segment_no: 2,
          run_id: job.runId,
          phase: "overview",
        }),
      )
      this.publish(job, "view.snapshot")
      return
    }
    if (job.stage === 3) {
      this.completeCurrent(job, "主线是把重复操作存成流程。")
      job.seq += 1
      job.phase = "investigate"
      job.steps.push(
        step({
          id: `${job.id}-menu`,
          title: "正在查看菜单展开后的画面",
          purpose: "确认选项名称。",
          status: "running",
          first_seq: job.seq,
          last_seq: job.seq,
          segment_no: 2,
          run_id: job.runId!,
          phase: "investigate",
          source_range: { start_ms: 310_000, end_ms: 330_000 },
        }),
      )
      this.publish(job, "view.snapshot")
      return
    }
    if (job.stage === 4) {
      const current = job.steps[job.steps.length - 1]
      job.seq += 1
      job.steps[job.steps.length - 1] = {
        ...current,
        status: "completed",
        last_seq: job.seq,
        finished_at: now(),
        frame_ids: ["frm_closed", "frm_open"],
        frames: [frameClosed, frameOpen],
        outcome: {
          message: "看到添加步骤、保存流程、运行。观察已记录。",
          evidence_status: "supported",
        },
      }
      this.publish(job, "activity.upsert")
      job.seq += 1
      job.phase = "compose"
      job.steps.push(
        step({
          id: `${job.id}-write`,
          title: "正在组织图文文章",
          purpose: "把已确认的步骤写成可阅读的段落。",
          status: "running",
          first_seq: job.seq,
          last_seq: job.seq,
          segment_no: 2,
          run_id: job.runId!,
          phase: "compose",
        }),
      )
      this.publish(job, "view.snapshot")
      return
    }
    this.completeCurrent(job, "已写成可阅读的文章。")
    job.status = "completed"
    job.articleId = article.id
    job.revision += 1
    job.runId = null
    job.seq = 0
    job.phase = null
    job.runStatus = "succeeded"
    this.publish(job, "view.snapshot")
  }

  private completeCurrent(job: Job, message: string) {
    const index = job.steps.length - 1
    job.steps[index] = {
      ...job.steps[index],
      status: "completed",
      finished_at: now(),
      last_seq: job.seq,
      outcome: { message, evidence_status: "supported" },
    }
  }

  private cancelJob(job: Job) {
    if (job.status === "completed") return this.requestView(job)
    if (!terminal(job.status)) {
      if (job.timer) clearTimeout(job.timer)
      job.timer = null
      job.status = "canceled"
      job.revision += 1
      job.runId = null
      job.seq = 0
      job.steps = job.steps.map((item) =>
        item.status === "running" || item.status === "queued"
          ? {
              ...item,
              status: "canceled",
              finished_at: now(),
              outcome: {
                message: "取消时尚未完成",
                evidence_status: "not_assessed",
              },
            }
          : item,
      )
      job.updatedAt = now()
      this.publish(job, "view.snapshot")
    }
    return this.requestView(job)
  }

  private retryJob(job: Job, key: string) {
    if (job.status === "completed" || (job.status !== "failed" && job.status !== "canceled")) {
      throw new ApiError({
        status: 409,
        code: "RETRY_NOT_ALLOWED",
        detail: "这次生成不能从这里重试。",
      })
    }
    const created = this.createRequest(job.inputUrl, key)
    const next = this.mustJob(created.body.request.id)
    next.retryOf = job.id
    return { ...created, body: this.requestView(next) }
  }

  private article(job: Job): ArticleResponse {
    if (job.status !== "completed" || !job.articleId)
      throw new ApiError({
        status: 409,
        code: "ARTICLE_NOT_READY",
        detail: "文章还没有发布。",
      })
    return {
      schema_version: 1,
      article: {
        ...article,
        source: job.source ?? article.source,
        published_at: job.updatedAt,
      },
      generation_model: developmentModel,
      frames,
      media: {
        status: "ready",
        can_inspect_now: true,
        can_restore_for_question: true,
      },
    }
  }

  private conversation(job: Job) {
    if (job.status !== "completed")
      throw new ApiError({
        status: 409,
        code: "ARTICLE_NOT_READY",
        detail: "文章还没有发布。",
      })
    let conversation = [...this.conversations.values()].find((item) => item.request_id === job.id)
    if (!conversation) {
      conversation = {
        id: `con_${job.id}`,
        request_id: job.id,
        article_id: article.id,
        snapshot_id: "snap_mock",
        last_turn_id: null,
        active_turn_id: null,
        created_at: now(),
      }
      this.conversations.set(conversation.id, conversation)
    }
    return { schema_version: 1, conversation }
  }

  private createTurn(
    conversationId: string,
    key: string,
    input: {
      question: string
      expected_last_turn_id: string | null
      context: QuestionContext
    },
  ) {
    const conversation = this.conversations.get(conversationId)
    if (!conversation)
      throw new ApiError({
        status: 404,
        code: "RESOURCE_NOT_FOUND",
        detail: "资源不存在。",
      })
    const body = JSON.stringify(input)
    const seen = this.turnKeys.get(`${conversationId}:${key}`)
    if (seen) {
      if (seen.body !== body)
        throw new ApiError({
          status: 409,
          code: "IDEMPOTENCY_CONFLICT",
          detail: "同一次提交的内容不一致。",
        })
      return this.accepted(this.turnView(this.mustTurn(seen.id)), true)
    }
    const question = input.question?.trim() ?? ""
    if (!question)
      throw new ApiError({
        status: 400,
        code: "INVALID_ARGUMENT",
        detail: "先写下一个问题。",
      })
    if ([...question].length > 4000)
      throw new ApiError({
        status: 422,
        code: "INPUT_LIMIT_EXCEEDED",
        detail: "这个问题太长了。",
      })
    if (input.expected_last_turn_id !== conversation.last_turn_id) {
      throw new ApiError({
        status: 409,
        code: "TURN_HISTORY_CHANGED",
        detail: "对话已经有了更新的一轮。",
      })
    }
    if (conversation.active_turn_id)
      throw new ApiError({
        status: 409,
        code: "TURN_IN_PROGRESS",
        detail: "上一轮回答仍在进行。",
        meta: { active_turn_id: conversation.active_turn_id },
      })
    this.assertContext(input.context)
    const turnNo = [...this.turns.values()].filter((item) => item.conversationId === conversationId).length + 1
    const id = `turn_${++this.seq}`
    const talk: Talk = {
      id,
      conversationId,
      requestId: conversation.request_id,
      turnNo,
      revision: 1,
      status: "queued",
      question,
      context: input.context ?? { frame_id: null, passage: null },
      retryOf: null,
      answer: null,
      failure: null,
      runId: `${id}-answer`,
      runStatus: "queued",
      phase: null,
      wait: "dispatch",
      seq: 1,
      steps: [],
      stage: 0,
      createdAt: now(),
      updatedAt: now(),
      timer: null,
    }
    this.turns.set(id, talk)
    conversation.last_turn_id = id
    conversation.active_turn_id = id
    this.turnKeys.set(`${conversationId}:${key}`, { body, id })
    this.kickTurn(talk)
    return this.accepted(this.turnView(talk), false)
  }

  private assertContext(context: QuestionContext | undefined) {
    const frameId = context?.frame_id
    if (frameId && !frames.some((item) => item.id === frameId)) {
      throw new ApiError({
        status: 422,
        code: "INVALID_CONTEXT",
        detail: "这张画面不属于当前视频。",
      })
    }
    const passage = context?.passage
    if (!passage) return
    const block = article.sections
      .flatMap((section) => section.blocks.map((item) => ({ section, item })))
      .find((item) => item.item.id === passage.block_id && item.section.id === passage.section_id)
    if (!block || !quoteMatchesBlock(block.item, passage.quote)) {
      throw new ApiError({
        status: 422,
        code: "INVALID_CONTEXT",
        detail: "选中的文字不在这一段里。",
      })
    }
  }

  private stepTurn(talk: Talk) {
    if (terminal(talk.status) || talk.status === "cancel_requested") {
      if (talk.status === "cancel_requested") this.finishCancel(talk)
      return
    }
    talk.updatedAt = now()
    talk.stage += 1
    const picked = frames.find((item) => item.id === talk.context.frame_id) ?? frameOpen
    if (talk.stage === 1) {
      talk.status = "running"
      talk.runStatus = "running"
      talk.wait = null
      talk.phase = "answer"
      talk.seq = 2
      talk.steps.push(
        step({
          id: `${talk.id}-read`,
          title: talk.context.passage ? "正在阅读选中的段落" : "正在阅读相关段落",
          purpose: "对照文章和这条视频。",
          status: "running",
          first_seq: 2,
          last_seq: 2,
          segment_no: 0,
          run_id: talk.runId,
          phase: "answer",
        }),
      )
      this.publish(talk, "view.snapshot")
      return
    }
    if (talk.stage === 2) {
      talk.steps[0] = {
        ...talk.steps[0],
        status: "completed",
        finished_at: now(),
        outcome: { message: "已对照文章原文。", evidence_status: "supported" },
      }
      talk.seq = 3
      talk.steps.push(
        step({
          id: `${talk.id}-look`,
          title: `正在查看 ${picked.caption}`,
          purpose: "核对画面里能确认的内容。",
          status: "completed",
          first_seq: 3,
          last_seq: 3,
          segment_no: 0,
          run_id: talk.runId,
          phase: "answer",
          frame_ids: [picked.id],
          frames: [picked],
          finished_at: now(),
          outcome: { message: picked.caption, evidence_status: "supported" },
        }),
      )
      this.publish(talk, "activity.upsert")
      return
    }
    talk.status = "completed"
    talk.runStatus = "succeeded"
    talk.phase = "answer"
    talk.seq = 4
    talk.revision += 1
    talk.answer = this.answerFor(talk, picked)
    const conversation = this.conversations.get(talk.conversationId)
    if (conversation?.active_turn_id === talk.id) conversation.active_turn_id = null
    this.publish(talk, "view.snapshot")
  }

  private answerFor(talk: Talk, picked: Frame): Answer {
    return {
      kind: "grounded",
      summary: "已核对这张画面",
      blocks: [
        {
          id: `${talk.id}-block`,
          type: "paragraph",
          origin: "video",
          content: [
            {
              type: "text",
              text:
                picked.id === frameClosed.id
                  ? "这一帧还停在编辑状态，菜单没有展开，不能据此确认流程已经运行。"
                  : "这一帧能看到展开后的菜单，里面有添加步骤、保存流程和运行。它说明的是可添加的步骤，还不是运行结果。",
              marks: [],
            },
            { type: "citation", citation_id: "ans_cite" },
          ],
        },
      ],
      citations: {
        ans_cite: {
          id: "ans_cite",
          kind: "video_frame",
          label: picked.caption,
          snapshot_id: "snap_mock",
          frame_id: picked.id,
          time_ms: picked.time_ms,
        },
      },
      frames: [picked],
      limitations: [
        {
          code: "not_observed",
          message: "该帧未覆盖运行完成后的结果页。",
          section_ids: ["cautions"],
          citation_ids: ["ans_cite"],
        },
      ],
    }
  }

  private cancelTurn(talk: Talk) {
    if (terminal(talk.status)) return this.turnView(talk)
    if (talk.timer) clearTimeout(talk.timer)
    talk.status = "cancel_requested"
    talk.runStatus = "cancel_requested"
    talk.updatedAt = now()
    talk.revision += 1
    this.publish(talk, "view.snapshot")
    if (this.auto) talk.timer = setTimeout(() => this.finishCancel(talk), this.intervalMs)
    return this.turnView(talk)
  }

  private finishCancel(talk: Talk) {
    if (talk.status !== "cancel_requested") return
    talk.status = "canceled"
    talk.runStatus = "canceled"
    talk.revision += 1
    talk.steps = talk.steps.map((item) => (item.status === "running" ? { ...item, status: "canceled", finished_at: now() } : item))
    const conversation = this.conversations.get(talk.conversationId)
    if (conversation?.active_turn_id === talk.id) conversation.active_turn_id = null
    talk.updatedAt = now()
    this.publish(talk, "view.snapshot")
  }

  private retryTurn(conversationId: string, talk: Talk, key: string, expected: string | null) {
    if (talk.status !== "failed" && talk.status !== "canceled")
      throw new ApiError({
        status: 409,
        code: "RETRY_NOT_ALLOWED",
        detail: "这一轮不能重试。",
      })
    const created = this.createTurn(conversationId, key, {
      question: talk.question,
      expected_last_turn_id: expected,
      context: talk.context,
    })
    const next = this.mustTurn(created.body.turn.id)
    next.retryOf = talk.id
    return { ...created, body: this.turnView(next) }
  }

  private kick(job: Job) {
    if (!this.auto) return
    const tick = () => {
      if (terminal(job.status)) return
      this.stepJob(job)
      if (!terminal(job.status)) job.timer = setTimeout(tick, this.intervalMs)
    }
    job.timer = setTimeout(tick, this.intervalMs)
  }

  private kickTurn(talk: Talk) {
    if (!this.auto) return
    const tick = () => {
      if (terminal(talk.status)) return
      this.stepTurn(talk)
      if (!terminal(talk.status)) talk.timer = setTimeout(tick, this.intervalMs)
    }
    talk.timer = setTimeout(tick, this.intervalMs)
  }

  private requestView(job: Job): RequestView {
    const current = [...job.steps].reverse().find((item) => item.status === "running")
    const recent = job.steps.slice(-8)
    return {
      schema_version: 1,
      request: {
        id: job.id,
        revision: job.revision,
        input_url: job.inputUrl,
        generation_model: developmentModel,
        status: job.status,
        reuse: job.reuse,
        retry_of_id: job.retryOf,
        article_id: job.articleId,
        created_at: job.createdAt,
        updated_at: job.updatedAt,
      },
      source: job.source,
      execution: job.runId
        ? {
            id: job.runId,
            kind: job.runKind,
            status: job.runStatus as "running",
            phase: job.phase as "overview" | null,
            retry_at: null,
            wait_reason: job.wait as "dispatch" | null,
          }
        : null,
      activity: {
        current_step_id: !terminal(job.status) && current ? current.id : null,
        recent_steps: recent,
        has_more: job.steps.length > recent.length,
        history_url: `/api/v1/requests/${job.id}/activity`,
        history_status: job.steps.length ? "available" : "none",
        history_expires_at: null,
      },
      failure: job.failure,
      actions: {
        can_cancel: !terminal(job.status),
        can_retry: job.status === "canceled" || job.status === "failed",
        can_read_article: job.articleId != null,
      },
      stream: {
        ...this.position(job),
        url: `/api/v1/requests/${job.id}/events`,
      },
    }
  }

  private turnView(talk: Talk): TurnView {
    const current = [...talk.steps].reverse().find((item) => item.status === "running")
    const conversation = this.conversations.get(talk.conversationId)
    const latest = conversation?.last_turn_id === talk.id
    return {
      schema_version: 1,
      turn: {
        id: talk.id,
        conversation_id: talk.conversationId,
        request_id: talk.requestId,
        article_id: article.id,
        snapshot_id: "snap_mock",
        turn_no: talk.turnNo,
        revision: talk.revision,
        status: talk.status,
        question: talk.question,
        context: talk.context,
        retry_of_id: talk.retryOf,
        answer: talk.answer,
        created_at: talk.createdAt,
        updated_at: talk.updatedAt,
      },
      execution: {
        id: talk.runId,
        kind: "answer.generate",
        status: talk.runStatus as NonNullable<TurnView["execution"]>["status"],
        phase: (talk.phase as NonNullable<TurnView["execution"]>["phase"]) ?? null,
        retry_at: null,
        wait_reason: (talk.wait as NonNullable<TurnView["execution"]>["wait_reason"]) ?? null,
      },
      activity: {
        current_step_id: !terminal(talk.status) && current ? current.id : null,
        recent_steps: talk.steps,
        has_more: false,
        history_url: `/api/v1/conversations/${talk.conversationId}/turns/${talk.id}/activity`,
        history_status: talk.steps.length ? "available" : "none",
        history_expires_at: null,
      },
      failure: talk.failure,
      actions: {
        can_cancel: !terminal(talk.status) && talk.status !== "cancel_requested",
        can_retry: latest && (talk.status === "canceled" || talk.status === "failed"),
        can_submit_next: latest && terminal(talk.status),
      },
      stream: {
        ...this.position(talk),
        url: `/api/v1/conversations/${talk.conversationId}/turns/${talk.id}/events`,
      },
    }
  }

  private position(target: Job | Talk) {
    return {
      cursor: `mock:${target.id}:${target.revision}:${target.runId ?? "-"}:${target.seq}`,
      revision: target.revision,
      run_id: target.runId,
      seq: target.seq,
    }
  }

  private statusOf(target: Job | Talk) {
    return target.status
  }

  private publish(target: Job | Talk, type: "view.snapshot" | "activity.upsert") {
    for (const socket of this.sockets) {
      const parsed = new URL(socket.url, "http://vidlens.local")
      const owner = this.target(parsed.pathname)
      if (owner !== target) continue
      this.send(socket, target, type)
    }
  }

  private send(socket: MockSocket, target: Job | Talk, type: "view.snapshot" | "activity.upsert") {
    const position = this.position(target)
    const scope = "inputUrl" in target ? { type: "request", id: target.id } : { type: "turn", id: target.id }
    if (type === "activity.upsert") {
      const current = target.steps.find((item) => item.last_seq === target.seq)
      if (!current) {
        type = "view.snapshot"
      } else {
        socket.emit(type, {
          schema_version: 1,
          scope,
          position,
          payload: {
            step: current,
            current_step_id: current.status === "running" ? current.id : null,
          },
        })
        return
      }
    }
    socket.emit("view.snapshot", {
      schema_version: 1,
      scope,
      position,
      payload: "inputUrl" in target ? this.requestView(target) : this.turnView(target),
    })
  }

  private target(pathname: string): Job | Talk | null {
    const request = pathname.match(/^\/api\/v1\/requests\/([^/]+)\/events$/)
    if (request) return this.requests.get(decodeURIComponent(request[1])) ?? null
    const turn = pathname.match(/^\/api\/v1\/conversations\/([^/]+)\/turns\/([^/]+)\/events$/)
    if (turn) return this.turns.get(decodeURIComponent(turn[2])) ?? null
    return null
  }

  private mustJob(id: string) {
    const job = this.requests.get(id)
    if (!job)
      throw new ApiError({
        status: 404,
        code: "RESOURCE_NOT_FOUND",
        detail: "资源不存在。",
      })
    return job
  }

  private mustTurn(id: string) {
    const turn = this.turns.get(id)
    if (!turn)
      throw new ApiError({
        status: 404,
        code: "RESOURCE_NOT_FOUND",
        detail: "资源不存在。",
      })
    return turn
  }

  private accepted<T>(body: T, replayed: boolean) {
    return {
      status: replayed ? 200 : 201,
      replayed,
      expiresAt: "2026-10-16T00:00:00Z",
      location: null,
      body,
    }
  }
}
