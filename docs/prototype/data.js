/* ============================================================
   映知 VidLens 原型 · 模拟数据 (全部为演示数据, 非真实后端返回)
   演示视频: Oceans 片段 (video.js 示例素材), 帧描述均按真实画面撰写
   ============================================================ */

/* 演示播放源: 用真实截帧在本地合成的 46 秒短片 (assets/demo-reel.mp4), 离线可用 */
const DEMO_VIDEO_URL = "./assets/demo-reel.mp4";

const VIDEOS = [
  {
    id: 12,
    title: "海洋 · 捕食链实拍片段 (示例素材)",
    durationMs: 46613,
    size: "23.0 MB",
    source: "url",
    status: "indexed",
    stateText: "已索引",
    statusChip: "chip-ok",
    demo: true,
    hasTranscript: true,
    hasVisual: true,
    hasSummary: true,
    chunks: 19,
    updated: "2 小时前",
    added: "2026-09-03",
  },
  {
    id: 14,
    title: "GTC 2026 主题演讲 · 现场实录",
    durationMs: 4364000,
    size: "1.2 GB",
    source: "url",
    status: "transcribing",
    stateText: "转写中",
    statusChip: "chip-warn",
    hasTranscript: false,
    hasVisual: false,
    hasSummary: false,
    chunkDone: 17,
    chunkTotal: 24,
    progress: 0.71,
    stageText: "ASR 分片 17 / 24 · worker 并发 3",
    updated: "刚刚",
    added: "2026-09-05",
  },
  {
    id: 11,
    title: "Transformer 从零讲解 · 第 7 讲:注意力机制",
    durationMs: 3500000,
    size: "640 MB",
    source: "upload",
    status: "indexed",
    stateText: "已索引",
    statusChip: "chip-ok",
    hasTranscript: true,
    hasVisual: false,
    hasSummary: true,
    chunks: 58,
    updated: "昨天",
    added: "2026-09-01",
  },
  {
    id: 9,
    title: "一席 · 陈曦:城市里的自然观察",
    durationMs: 1683000,
    size: "310 MB",
    source: "upload",
    status: "completed",
    stateText: "待建索引",
    statusChip: "chip-info",
    hasTranscript: true,
    hasVisual: false,
    hasSummary: true,
    chunks: 0,
    updated: "周四",
    added: "2026-08-28",
  },
  {
    id: 7,
    title: "RustConf 2025 · 异步运行时内幕",
    durationMs: 3271000,
    size: "720 MB",
    source: "url",
    status: "indexed",
    stateText: "已索引",
    statusChip: "chip-ok",
    hasTranscript: true,
    hasVisual: false,
    hasSummary: true,
    chunks: 52,
    updated: "上周",
    added: "2026-08-24",
  },
  {
    id: 5,
    title: "产品发布会 Q&A 实录",
    durationMs: 2472000,
    size: "458 MB",
    source: "upload",
    status: "failed",
    stateText: "转写失败",
    statusChip: "chip-bad",
    hasTranscript: false,
    hasVisual: false,
    hasSummary: false,
    errText: "ASR 分片 6 失败已重试 2 次:provider 429,重试预算耗尽",
    updated: "周一",
    added: "2026-08-31",
  },
];

const KBS = [
  {
    id: 3,
    name: "AI 前沿追踪",
    desc: "跟踪各家模型发布与技术演讲,沉淀成可跨视频检索的资料库,回答一律带时间点引用。",
    videoCount: 12,
    updated: "2 小时前",
    members: [14, 11, 7, 12],
  },
  {
    id: 2,
    name: "公开课 · 深度学习基础",
    desc: "按讲次整理的公开课合集,适合按概念跨讲次提问。",
    videoCount: 8,
    updated: "3 天前",
    members: [11, 9],
  },
  {
    id: 1,
    name: "纪录片素材库",
    desc: "长镜头素材为主,包含纯画面 (无解说) 视频,可验证 OCR 与画面描述证据。",
    videoCount: 5,
    updated: "上周",
    members: [12],
  },
];

const SESSIONS = [
  { id: 91, q: "发布会里提到的新芯片能效提升是多少?", where: "AI 前沿追踪", when: "2 小时前", kb: 3 },
  { id: 92, q: "第 7 讲里 Q 和 K 的维度为什么可以不一致?", where: "第 7 讲:注意力机制", when: "昨天", video: 11 },
  { id: 93, q: "这段素材里鲨鱼是什么时候出现的?", where: "海洋 · 捕食链实拍片段", when: "昨天", video: 12 },
  { id: 94, q: "城市观察者提到的三个口袋公园案例分别在哪?", where: "一席 · 城市里的自然观察", when: "周四", video: 9 },
];

/* ---------- 视频 12 的解说轨 (示例字幕, 与真实画面顺序一致) ---------- */
const TRANSCRIPT12 = [
  { startMs: 800, endMs: 7000, text: "开阔的远海上空,成群的鲣鸟在盘旋,寻找水下的猎物。" },
  { startMs: 7500, endMs: 12500, text: "它们收拢翅膀,像箭一样扎进海水,溅起一片白色水花。" },
  { startMs: 13000, endMs: 17500, text: "水下,沙丁鱼聚成致密的鱼球,这是它们防御天敌的方式。" },
  { startMs: 18000, endMs: 22500, text: "鱼球越缩越紧,可捕食者还是从四面八方赶了过来。" },
  { startMs: 23000, endMs: 27000, text: "鲣鸟入水后并不急着返回水面,它们会贴着鱼群追击几秒。" },
  { startMs: 27500, endMs: 30500, text: "真正的大家伙到了:一条鲨鱼从鱼球的边缘冲了进去。" },
  { startMs: 31000, endMs: 34500, text: "鲨鱼的冲撞把鱼群撕开一道口子,银色的鳞片四处飞散。" },
  { startMs: 38800, endMs: 40800, text: "混乱中,两只海豚并排穿过画面,它们似乎也盯着这群沙丁鱼。" },
  { startMs: 41300, endMs: 44300, text: "最后,一头座头鲸张着嘴冲出水面,吞下大片鱼群,又重重地摔回海里。" },
  { startMs: 44500, endMs: 46200, text: "浪声渐落,这场围绕鱼球的狂欢暂时告一段落。" },
];

/* ---------- 视频 12 的已索引视觉帧 (画面描述, 按真实画面撰写) ---------- */
const FRAMES12 = [
  {
    id: "f-004", timeMs: 4000, endMs: 4600,
    ocr: "",
    caption: "两只鲣鸟收拢翅膀俯冲,身体拉成流线型,其中一只已经贴近海面。",
    method: "caption",
  },
  {
    id: "f-005", timeMs: 5200, endMs: 5800,
    ocr: "",
    caption: "水下视角:阳光穿过起伏的海面,形成一道道向下的光柱。",
    method: "caption",
  },
  {
    id: "f-015", timeMs: 15600, endMs: 16200,
    ocr: "",
    caption: "沙丁鱼聚成致密的深色鱼球,在光柱下隐约可见边缘翻涌。",
    method: "caption",
  },
  {
    id: "f-023", timeMs: 23500, endMs: 24100,
    ocr: "",
    caption: "海面上鸟群密集,入水点激起成片的白色水花。",
    method: "caption",
  },
  {
    id: "f-031", timeMs: 31000, endMs: 31600,
    ocr: "",
    caption: "一条鲨鱼迎面冲进鱼球,胸鳍完全展开,身体周围是四散的小鱼。",
    method: "caption",
  },
  {
    id: "f-039", timeMs: 39500, endMs: 40100,
    ocr: "",
    caption: "海面翻起大片白色水花,有大型生物贴近水面游动。",
    method: "caption",
  },
  {
    id: "f-040", timeMs: 40200, endMs: 40800,
    ocr: "",
    caption: "两只海豚并排游过,身后拖着淡淡的气泡尾迹。",
    method: "caption",
  },
  {
    id: "f-043", timeMs: 43000, endMs: 43600,
    ocr: "",
    caption: "座头鲸侧身跃出水面,长鳍张开,身后掀起大片水花。",
    method: "caption",
  },
];

const SUMMARY12 = `这是一段约 47 秒的海洋实拍素材,记录了远海一场典型的捕食链,没有对白,情节由画面与音乐推进。

**画面主线**
- 鲣鸟在空中盘旋、俯冲入水;
- 沙丁鱼聚成致密的鱼球自保,鸟群在水面上持续围猎;
- 鲨鱼从鱼球边缘冲入,把鱼群撕开口子,海豚随后穿过画面;
- 最后座头鲸冲出水面吞下大片鱼群,落入水中收尾。

**适合的用法**
本片段是社区常用的测试素材:无解说时可以验证纯画面证据的召回表现;配上示例解说轨后,适合演示「口述与画面互相印证」的问答场景,例如核对捕食顺序、动物出现的时刻。`;

/* ---------- 视频 12 的 RAG 索引状态 ---------- */
const INDEX12 = {
  state: "indexed", stateText: "已索引", build: "build v3",
  sourceMap: "source-map-v2",
  chunker: "recursive-sentence-source-v2",
  chunks: 19,
  dims: 1024,
  model: "bge-m3",
  modalities: [
    { k: "transcript", n: 12, pct: 63 },
    { k: "visual_caption", n: 7, pct: 37 },
  ],
};

/* ---------- AI 服务配置 (BYOK) ---------- */
const AI_PROFILES = [
  { cap: "对话模型", icon: "cpu", model: "glm-4.6", base: "https://open.bigmodel.cn/api/paas/v4", state: "ok", stateText: "已验证", meta: "2 天前 · chat/completions" },
  { cap: "语音识别 ASR", icon: "activity", model: "whisper-large-v3", base: "https://api.siliconflow.cn/v1", state: "ok", stateText: "已验证", meta: "9 月 1 日 · audio/transcriptions" },
  { cap: "向量模型", icon: "layers", model: "bge-m3 · 1024 维", base: "https://api.siliconflow.cn/v1/embeddings", state: "ok", stateText: "已验证", meta: "9 月 1 日 · 维度已探测" },
  { cap: "重排序 Rerank", icon: "sort", model: "未配置", base: "确定性 rerank 生效中,模型 rerank 未启用", state: "mute", stateText: "未启用", meta: "可选 · /rerank 协议" },
  { cap: "视觉模型", icon: "photo", model: "qwen2.5-vl-72b", base: "https://api.siliconflow.cn/v1", state: "ok", stateText: "已验证", meta: "8 月 30 日 · 关键帧 OCR 与描述" },
];

const MEMORIES = [
  { id: 1, text: "关注推理成本与部署方案,对营销层面的发布信息兴趣不大。", scope: "user", scopeText: "用户", when: "3 天前 · 来源: 会话总结", importance: "高", status: "active" },
  { id: 2, text: "引用回答时尽量给出视频内的时间点,便于回看核对。", scope: "user", scopeText: "用户", when: "上周 · 来源: 人工添加", importance: "中", status: "active" },
  { id: 3, text: "GTC 相关问题优先指向《GTC 2026 主题演讲》。", scope: "knowledge_base", scopeText: "知识库 · AI 前沿追踪", when: "昨天 · 来源: 会话总结", importance: "中", status: "conflicted" },
  { id: 4, text: "示例视频《海洋》常用作画面证据的演示素材。", scope: "video", scopeText: "视频 · 海洋", when: "2 小时前 · 来源: 会话总结", importance: "低", status: "active" },
];

/* ---------- 脚本化的问答场景 ---------- */
/* 引用对象: c 引用编号, modality, 时间, 原文, 展示上下文 */
const SCEN_OCEAN_CHAIN = {
  match: ["顺序", "先后", "一致", "口述", "过程"],
  steps: [
    { kind: "retrieve", label: "检索转写片段", tool: "search_transcript", ms: 760,
      hits: { q: "捕食 顺序 鲣鸟 鱼球 鲨鱼 鲸", n: 6,
        rows: [
          { s: ".91", t: "transcript", v: "鲣鸟收拢翅膀,像箭一样扎进海水…", at: "00:07" },
          { s: ".87", t: "transcript", v: "沙丁鱼聚成致密的鱼球…", at: "00:13" },
          { s: ".83", t: "transcript", v: "一条鲨鱼从鱼球的边缘冲了进去", at: "00:27" },
          { s: ".78", t: "transcript", v: "座头鲸张着嘴冲出水面…", at: "00:41" },
          { s: ".74", t: "visual_caption", v: "座头鲸侧身跃出水面,长鳍张开…", at: "00:43" },
          { s: ".71", t: "visual_caption", v: "一条鲨鱼迎面冲进鱼球,胸鳍展开…", at: "00:31" },
        ] } },
  ],
  answer: [
    "按解说轨的叙述,捕食的先后顺序是:鲣鸟先从空中俯冲入水 ",
    { c: 1 },
    ",沙丁鱼随即聚成致密的鱼球自保 ",
    { c: 2 },
    ";接着鲨鱼从鱼球边缘冲入,把鱼群撕开口子 ",
    { c: 3 },
    ";最后座头鲸整口吞下大片鱼群,摔回海里收尾 ",
    { c: 4 },
    "。\n\n口述与画面的核对结果相当一致:俯冲入水、鲨鱼冲入、鲸鱼破水这三个关键时刻都有已索引的画面描述证据 ",
    { c: 5 }, { c: 6 }, { c: 7 },
    ",且独立核验未发现反例。\n\n唯一对不上的是一处细节:解说称海豚「似乎也盯着这群沙丁鱼」,但已索引帧只拍到它们游过画面,没有拍到捕食动作,这一点画面无法证实 ",
    { c: 8 },
    "。",
  ],
  citations: [
    { c: 1, modality: "transcript", startMs: 7500, endMs: 12500, status: "exact", score: ".91",
      quote: "它们收拢翅膀,像箭一样扎进海水,溅起一片白色水花。",
      ctx: "承接 00:00 鲣鸟盘旋一段。", task: 12, ev: "ev-9f21a4", src: "hybrid" },
    { c: 2, modality: "transcript", startMs: 13000, endMs: 17500, status: "exact", score: ".87",
      quote: "水下,沙丁鱼聚成致密的鱼球,这是它们防御天敌的方式。",
      ctx: "视角从海面切到水下。", task: 12, ev: "ev-9f21b7", src: "hybrid" },
    { c: 3, modality: "transcript", startMs: 27500, endMs: 30500, status: "exact", score: ".83",
      quote: "真正的大家伙到了:一条鲨鱼从鱼球的边缘冲了进去。",
      ctx: "鲨鱼首次入画前的解说。", task: 12, ev: "ev-9f21c2", src: "vector" },
    { c: 4, modality: "transcript", startMs: 41300, endMs: 44300, status: "exact", score: ".78",
      quote: "最后,一头座头鲸张着嘴冲出水面,吞下大片鱼群,又重重地摔回海里。",
      ctx: "结尾高潮段落。", task: 12, ev: "ev-9f21d8", src: "vector" },
    { c: 5, modality: "visual_caption", startMs: 4000, endMs: 4600, status: "exact", score: ".77",
      quote: "两只鲣鸟收拢翅膀俯冲,身体拉成流线型。",
      ctx: "来自已索引关键帧,帧 ID f-004。", task: 12, ev: "ev-8d03aa", src: "vector", frame: 4000 },
    { c: 6, modality: "visual_caption", startMs: 31000, endMs: 31600, status: "exact", score: ".74",
      quote: "一条鲨鱼迎面冲进鱼球,胸鳍完全展开。",
      ctx: "来自已索引关键帧,帧 ID f-031。", task: 12, ev: "ev-8d03be", src: "vector", frame: 31000 },
    { c: 7, modality: "visual_caption", startMs: 43000, endMs: 43600, status: "exact", score: ".71",
      quote: "座头鲸侧身跃出水面,长鳍张开,身后掀起大片水花。",
      ctx: "来自已索引关键帧,帧 ID f-043。", task: 12, ev: "ev-8d03cf", src: "vector", frame: 43000 },
    { c: 8, modality: "transcript", startMs: 38800, endMs: 40800, status: "exact", score: ".66",
      quote: "混乱中,两只海豚并排穿过画面,它们似乎也盯着这群沙丁鱼。",
      ctx: "解说用「似乎」表述,画面未拍到捕食动作。", task: 12, ev: "ev-9f21e1", src: "vector" },
  ],
  claims: [
    { text: "捕食顺序为:鲣鸟俯冲、鱼球收拢、鲨鱼冲入、座头鲸吞食。", status: "verified", conf: 0.93,
      evs: [1, 2, 3, 4, 5, 6, 7],
      note: "解说与三处关键帧相互印证,时间范围可回放。",
      inspect: { result: "support", reason: "cited evidence supports the complete claim; bounded counter-search found no contradiction",
        counterQuery: "鲣鸟 俯冲 顺序 鲸鱼 最后", searchCompleted: true } },
    { text: "海豚也在捕食这群沙丁鱼。", status: "uncertain", conf: 0.38,
      evs: [8],
      note: "仅解说提及,且原句用了「似乎」;画面帧未拍到捕食动作。",
      inspect: { result: "insufficient", reason: "complete claim, citation binding, or pixel verification is not sufficiently supported",
        counterQuery: "海豚 捕食 沙丁鱼 画面", searchCompleted: true } },
    { text: "座头鲸把整片鱼群一扫而空。", status: "corrected", conf: 0.3,
      evs: [4],
      note: "人工更正:解说只说「吞下大片鱼群」,没有说清空;更正后不再使用该表述。",
      inspect: { result: "insufficient", reason: "at least one text or pixel evidence source contradicts the claim",
        counterQuery: "座头鲸 吞食 鱼群 数量", searchCompleted: true } },
  ],
};

const SCEN_OCEAN_WHALE = {
  match: ["鲸", "第几秒", "什么时候", "哪一秒"],
  steps: [
    { kind: "retrieve", label: "检索转写片段", tool: "search_transcript", ms: 620,
      hits: { q: "座头鲸 出现 破水 吞食", n: 4,
        rows: [
          { s: ".93", t: "transcript", v: "座头鲸张着嘴冲出水面,吞下大片鱼群…", at: "00:41" },
          { s: ".78", t: "visual_caption", v: "座头鲸侧身跃出水面,长鳍张开…", at: "00:43" },
          { s: ".71", t: "transcript", v: "浪声渐落,这场狂欢告一段落", at: "00:46" },
        ] } },
  ],
  answer: [
    "座头鲸首次出现在 00:41 的解说窗口里 ",
    { c: 1 },
    ",已索引的画面帧确认 00:43 它侧身跃出水面 ",
    { c: 2 },
    "。捕食方式按解说是「张着嘴冲出水面,吞下大片鱼群」,与画面中的破水动作一致,独立核验未发现反例。\n\n注意:以上时间来自解说窗口,是粗粒度时间,不是逐字时间戳。",
  ],
  citations: [
    { c: 1, modality: "transcript", startMs: 41300, endMs: 44300, status: "exact", score: ".93",
      quote: "最后,一头座头鲸张着嘴冲出水面,吞下大片鱼群,又重重地摔回海里。",
      ctx: "结尾高潮段落。", task: 12, ev: "ev-4c81d0", src: "vector" },
    { c: 2, modality: "visual_caption", startMs: 43000, endMs: 43600, status: "exact", score: ".72",
      quote: "座头鲸侧身跃出水面,长鳍张开,身后掀起大片水花。",
      ctx: "来自已索引关键帧,帧 ID f-043。", task: 12, ev: "ev-3a77f1", src: "vector", frame: 43000 },
  ],
  claims: [
    { text: "座头鲸在 00:41 前后出现,以冲出水面吞食的方式捕食。", status: "verified", conf: 0.9,
      evs: [1, 2],
      note: "解说与关键帧共同支撑。",
      inspect: { result: "support", reason: "cited evidence supports the complete claim; bounded counter-search found no contradiction",
        counterQuery: "座头鲸 出现 时间 海豚 之后", searchCompleted: true } },
    { text: "鲸鱼出现的准确时刻是 00:41.0。", status: "uncertain", conf: 0.4,
      evs: [1],
      note: "解说窗口是粗粒度时间,不能当作逐字时间戳。",
      inspect: { result: "insufficient", reason: "temporal boundary is coarse, not a verbatim timestamp",
        counterQuery: "座头鲸 38 秒 精确", searchCompleted: true } },
  ],
};

const SCEN_OCEAN_THEME = {
  match: ["讲了什么", "内容", "大概", "主题", "故事"],
  steps: [
    { kind: "retrieve", label: "检索转写片段", tool: "search_transcript", ms: 640,
      hits: { q: "素材 主题 概述", n: 5,
        rows: [
          { s: ".85", t: "transcript", v: "成群的鲣鸟在盘旋,寻找水下的猎物", at: "00:00" },
          { s: ".81", t: "transcript", v: "沙丁鱼聚成致密的鱼球", at: "00:13" },
          { s: ".79", t: "transcript", v: "座头鲸…重重地摔回海里", at: "00:41" },
        ] } },
  ],
  answer: [
    "这段 47 秒的素材记录了远海一场完整的捕食链:鲣鸟从空中俯冲入水 ",
    { c: 1 },
    ",沙丁鱼聚成鱼球自保 ",
    { c: 2 },
    ",鲨鱼和海豚随后赶到 ",
    { c: 3 },
    ",最后由一头座头鲸收尾 ",
    { c: 4 },
    "。全片没有对白,以上内容来自示例解说字幕轨。",
  ],
  citations: [
    { c: 1, modality: "transcript", startMs: 800, endMs: 7000, status: "exact", score: ".85",
      quote: "开阔的远海上空,成群的鲣鸟在盘旋,寻找水下的猎物。",
      ctx: "开场段落。", task: 12, ev: "ev-112233", src: "vector" },
    { c: 2, modality: "transcript", startMs: 13000, endMs: 17500, status: "exact", score: ".81",
      quote: "水下,沙丁鱼聚成致密的鱼球,这是它们防御天敌的方式。",
      ctx: "鱼球首次被提到。", task: 12, ev: "ev-112244", src: "vector" },
    { c: 3, modality: "transcript", startMs: 27500, endMs: 34500, status: "exact", score: ".77",
      quote: "一条鲨鱼从鱼球的边缘冲了进去。鲨鱼的冲撞把鱼群撕开一道口子。",
      ctx: "合并了两条相邻解说。", task: 12, ev: "ev-112255", src: "hybrid" },
    { c: 4, modality: "transcript", startMs: 41300, endMs: 44300, status: "exact", score: ".79",
      quote: "一头座头鲸张着嘴冲出水面,吞下大片鱼群,又重重地摔回海里。",
      ctx: "结尾段落。", task: 12, ev: "ev-112266", src: "vector" },
  ],
  claims: [
    { text: "素材主线是围绕沙丁鱼球展开的多层捕食链。", status: "verified", conf: 0.9,
      evs: [1, 2, 3, 4], note: "解说轨多处一致。",
      inspect: { result: "support", reason: "cited evidence supports the complete claim; bounded counter-search found no contradiction",
        counterQuery: "海洋 纪录片 捕食链 鲨鱼 鲸鱼", searchCompleted: true } },
  ],
};

/* 深入研究场景 (mode=research, 受限 Planner 循环 + 查询时像素核验 + 发布阻断) */
const SCEN_OCEAN_DOLPHIN = {
  match: ["海豚"],
  researchOnly: true,
  blocked: true,
  steps: [
    { kind: "plan", label: "Planner 规划下一步", tool: "planner_llm", ms: 620,
      out: "需要先找到海豚出現的解说片段,再核对画面。" },
    { kind: "retrieve", label: "检索转写片段", tool: "search_transcript", ms: 720,
      hits: { q: "海豚 穿过 画面 沙丁鱼", n: 4,
        rows: [
          { s: ".84", t: "transcript", v: "两只海豚并排穿过画面,它们似乎也盯着…", at: "00:39" },
          { s: ".69", t: "transcript", v: "鲨鱼的冲撞把鱼群撕开一道口子…", at: "00:31" },
        ] } },
    { kind: "plan", label: "Planner 规划下一步", tool: "planner_llm", ms: 560,
      out: "解说只有「似乎也盯着」,需要读取该窗口的已索引帧确认。" },
    { kind: "tool", label: "读取已索引画面", tool: "inspect_visual_window", ms: 640,
      out: "读取 00:39 至 00:41 的已索引 observation:仅 1 帧画面描述 (f-040),看不到捕食动作,不足以判定。" },
    { kind: "plan", label: "Planner 规划下一步", tool: "planner_llm", ms: 600,
      out: "持久化 observation 不足以判定行为,按 seed 窗口做查询时像素核验,预算 3 帧。" },
    { kind: "tool", label: "查询时读取原始帧", tool: "investigate_visual", ms: 1600, frames: true,
      frameIds: ["f-039", "f-040"],
      out: "按 seed 窗口读取 2 帧原始像素 (00:39.5 / 00:40.2):两只海豚并排游过,嘴部无鱼,周围鱼群完整。" },
    { kind: "plan", label: "Planner 规划下一步", tool: "planner_llm", ms: 480,
      out: "像素观察不支持「正在捕食」,也不足以断言它只是路过;停止补检,构建结论。" },
  ],
  answer: [
    "现有证据不足或存在冲突,暂时无法确认。以下引用仅供核对,不代表结论已获支持。",
  ],
  blockedNote: "独立核验 (claim-inspector-v2-pixel) 未通过:像素观察不支持「正在捕食」,答案按策略阻断,不发布原结论。",
  citations: [
    { c: 1, modality: "transcript", startMs: 38800, endMs: 40800, status: "exact", score: ".84",
      quote: "混乱中,两只海豚并排穿过画面,它们似乎也盯着这群沙丁鱼。",
      ctx: "解说的「似乎」表述本身不确定。", task: 12, ev: "ev-d1a001", src: "vector" },
    { c: 2, modality: "visual_caption", startMs: 43000, endMs: 43600, status: "exact", score: ".58",
      quote: "两只海豚并排游过,身后拖着淡淡的气泡尾迹。",
      ctx: "已索引关键帧 f-040,恰在目标窗口内但无捕食动作。", task: 12, ev: "ev-d1a002", src: "vector", frame: 40200 },
  ],
  claims: [
    { text: "海豚正在捕食这群沙丁鱼。", status: "uncertain", conf: 0.3,
      evs: [1, 2],
      note: "解说只有「似乎」;查询时像素观察显示嘴部无鱼、鱼群完整,不构成捕食证据。",
      inspect: { result: "insufficient", reason: "pixel observation does not support feeding behavior; statement stays unproven",
        counterQuery: "海豚 捕食 沙丁鱼 嘴", searchCompleted: true, pixel: "2 帧 · 不支持" } },
    { text: "海豚只是恰好路过画面。", status: "uncertain", conf: 0.34,
      evs: [1, 2],
      note: "同样无法完全证明:单个瞬间不能证明整段行为的意图。",
      inspect: { result: "insufficient", reason: "a still frame cannot prove a sequence, causality or absence throughout a video",
        counterQuery: "海豚 捕食 鱼球 行为", searchCompleted: true, pixel: "2 帧 · 不支持" } },
  ],
};

/* 知识库场景 (跨视频) */
const SCEN_KB_COST = {
  match: ["成本", "推理", "优化"],
  steps: [
    { kind: "retrieve", label: "跨视频检索", tool: "search_transcript", ms: 980,
      hits: { q: "推理成本 优化 量化 缓存", n: 9, cross: 3,
        rows: [
          { s: ".88", t: "transcript", v: "GTC 2026 主题演讲 · 现场实录", at: "18:42" },
          { s: ".84", t: "transcript", v: "RustConf 2025 · 异步运行时内幕", at: "31:05" },
          { s: ".80", t: "transcript", v: "第 7 讲:注意力机制", at: "44:17" },
        ] } },
  ],
  answer: [
    "三场演讲各自提到的降本思路可以分成两层:\n\n**部署层**:GTC 2026 主题演讲里提到用 FP4 量化与 KV 缓存复用把单请求显存占用降下来 ",
    { c: 1 },
    ";**调度层**:RustConf 那场从运行时角度讲了任务切分与有界并发,避免长请求占满推理队列 ",
    { c: 2 },
    "。另外第 7 讲在讲注意力时顺带提到,长上下文下 KV 缓存是成本大头 ",
    { c: 3 },
    ",与 GTC 的优化方向互为印证。\n\n提醒一下:第一场视频仍在转写中,以上引用来自已完成的分片,后续分片完成后再问,答案可能更完整。",
  ],
  citations: [
    { c: 1, modality: "transcript", startMs: 1122000, endMs: 1138000, status: "coarse", score: ".88",
      quote: "通过 FP4 量化与 KV 缓存复用,单请求显存占用显著下降。",
      ctx: "演讲中段,推理成本章节。", task: 14, videoTitle: "GTC 2026 主题演讲 · 现场实录", ev: "ev-kb3101", src: "hybrid" },
    { c: 2, modality: "transcript", startMs: 1865000, endMs: 1880000, status: "coarse", score: ".84",
      quote: "把长请求切成有界的任务片段,推理队列才不会被打满。",
      ctx: "异步运行时章节。", task: 7, videoTitle: "RustConf 2025 · 异步运行时内幕", ev: "ev-kb3102", src: "vector" },
    { c: 3, modality: "transcript", startMs: 2657000, endMs: 2672000, status: "coarse", score: ".80",
      quote: "长上下文里,KV 缓存几乎决定了推理的显存下限。",
      ctx: "注意力机制讲解的延伸。", task: 11, videoTitle: "Transformer 从零讲解 · 第 7 讲", ev: "ev-kb3103", src: "vector" },
  ],
  claims: [
    { text: "三场演讲分别从部署、调度、注意力结构三个角度讨论了推理成本。", status: "verified", conf: 0.85,
      evs: [1, 2, 3], note: "跨视频引用,均来自已索引分片。" },
  ],
};

/* 标准问答 (strict_rag) 的兜底回答 */
function genericVideoAnswer(question, vid) {
  const v = VIDEOS.find((x) => x.id === vid);
  const row = v.demo ? TRANSCRIPT12[2] : TRANSCRIPT12[2];
  return {
    steps: [
      { kind: "retrieve", label: "检索转写片段", tool: "search_transcript", ms: 700,
        hits: { q: question.slice(0, 18), n: 4,
          rows: [
            { s: ".82", t: "transcript", v: row.text.slice(0, 18) + "…", at: fmtClock(row.startMs) },
            { s: ".71", t: "transcript", v: "开阔的远海上空,成群的鲣鸟在盘旋…", at: "00:00" },
          ] } },
    ],
    answer: [
      "在《" + v.title + "》的解说轨里,与「" + question + "」最相关的一段出现在 ",
      { c: 1 },
      "。本次是标准快速问答,只使用了转写文本,没有检查画面证据;如果这个问题需要看画面才能确认,可以切换到 Agent 检证再问一次。",
    ],
    citations: [
      { c: 1, modality: "transcript", startMs: row.startMs, endMs: row.endMs, status: "exact", score: ".82",
        quote: row.text, ctx: "当前检索窗内最相关的片段。", task: vid, ev: "ev-gen001", src: "vector" },
    ],
  };
}

function genericKbAnswer(question) {
  return {
    steps: [
      { kind: "retrieve", label: "跨视频检索", tool: "search_transcript", ms: 860,
        hits: { q: question.slice(0, 16), n: 6, cross: 3,
          rows: [
            { s: ".81", t: "transcript", v: "GTC 2026 主题演讲 · 现场实录", at: "18:42" },
            { s: ".76", t: "transcript", v: "RustConf 2025 · 异步运行时内幕", at: "31:05" },
            { s: ".72", t: "transcript", v: "第 7 讲:注意力机制", at: "44:17" },
          ] } },
    ],
    answer: [
      "围绕「" + question + "」,在知识库的 3 个视频里找到了相关片段,最相关的一段在 ",
      { c: 1 }, "。这是标准快速问答;需要跨视频对照或更深入的核验时,可以换一个更具体的问法。",
    ],
    citations: SCEN_KB_COST.citations,
  };
}

/* 证据漏斗固定八步 (服务端固定顺序, Planner 只做受限选择) */
const FUNNEL_STEPS = [
  "全局摘要与元数据",
  "transcript 检索",
  "Planner 决策 · 补哪个缺口",
  "时间窗扩展",
  "视觉 / OCR 候选",
  "视觉确认",
  "引用答案构建",
  "Evidence / Claim 校验",
];

const SUGGEST_VIDEO = [
  { icon: "shield-check", text: "这段视频里,捕食的先后顺序是什么?口述和画面一致吗?", sc: SCEN_OCEAN_CHAIN, mode: "agent" },
  { icon: "target", text: "鲸鱼是在第几秒出现的?它是怎么捕食的?", sc: SCEN_OCEAN_WHALE, mode: "agent" },
  { icon: "zoom-scan", text: "海豚到底有没有在捕食这群沙丁鱼?", sc: SCEN_OCEAN_DOLPHIN, mode: "research" },
  { icon: "message", text: "这段素材讲了一个什么主题?", sc: SCEN_OCEAN_THEME, mode: "strict" },
];

const SUGGEST_VIDEO_GENERIC = [
  { icon: "message", text: "这段视频的核心内容是什么?", sc: null },
  { icon: "target", text: "帮我划一下重点结论和出处。", sc: null },
];

const SUGGEST_KB = [
  { icon: "bolt", text: "几场演讲分别提到了哪些降低推理成本的思路?", sc: SCEN_KB_COST },
  { icon: "layers", text: "知识库里关于注意力机制都有哪些讲法?", sc: null },
];

function pickScenario(question, suggestions) {
  for (const s of suggestions) {
    if (!s.sc) continue;
    const hit = s.sc.match.filter((k) => question.includes(k)).length;
    if (hit > 0) return s.sc;
  }
  return null;
}
