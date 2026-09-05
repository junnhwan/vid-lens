# 映知 · 产品 UI 高保真原型

> 状态:交互原型,仅用于评审新前端的 UI 形态,不是生产代码,也不参与任何构建。
> 打开方式:直接双击 `index.html`(无需构建、无依赖);字体来自 Google Fonts,离线时自动回退系统字体。演示视频为本地资产,离线可完整演示。

## 原型覆盖的页面

| 路由 | 页面 | 演示重点 |
|------|------|----------|
| `#/dashboard` | 工作台 | 提问式首页、处理中任务、最近视频/会话 |
| `#/library` | 视频库 | 过滤器、转写进度、失败重试状态 |
| `#/kb` | 知识库 | 跨视频问答入口、成员视频速览 |
| `#/video/12` | 视频工作台 | 播放器 + 多模态时间轴 + 画面证据帧 + 索引状态 |
| `#/chat/v/12` | 单视频问答 | 四种问答模式、流式回答、可回放引用、证据账本 |
| `#/chat/kb/3` | 知识库问答 | 跨视频引用(仅快速问答,与后端当前能力一致) |
| `#/settings` | 设置 | BYOK AI 服务配置、记忆治理 |

## 值得点一点的东西

1. `#/chat/v/12` 的四条建议问题各对应一种模式(点击会自动切换模式):
   - **快速问答**:一次检索直接给引用;
   - **Agent 检证**:`search_transcript → build_cited_answer`,回答保存后经独立核验;
   - **深入研究 (实验)**:Planner 循环,演示 `investigate_visual` 查询时像素核验,以及**核验不通过时阻断发布**的回答形态;
   - **证据漏斗 (实验)**:固定八步漏斗的可视化。
2. 回答中的 `C#` 引用可点开证据详情(模态、毫秒范围、anchor quote、反例检索上下文),并可 **跳转视频回放**。
3. 证据账本:claim 状态、置信度、反例检索 (counter_query)、像素核验结果,以及对不确定判断的人工更正。
4. 视频工作台的时间轴把**解说转写、画面 OCR、画面描述**排在同一条可回放时间线上,点击即可寻址;画面证据帧从本地演示视频实时截取。

## 与后端能力的对应(已对照源码核验)

原型交互逐条对应 `internal/service` 与 `cmd/server/router.go` 的当前实现:

| 原型交互 | 后端事实 |
|---|---|
| 快速问答 (strict_rag) | `POST /chat/sessions/:id/messages/stream`,SSE 仅 `answer`/`citations`/`done` |
| Agent 检证 | `POST .../messages/agent/stream`,模板 direct_qa 实际步骤为 `search_transcript` → `build_cited_answer`;KB 会话被拒绝 |
| 独立核验 | `EvidenceInspector` (claim-inspector-v2-pixel):claim 拆分(≤8)、canonical evidence 解析、反例检索 (counter_query)、像素核验(≤8 帧);结论 support / contradict / insufficient,未通过时以「现有证据不足或存在冲突…」阻断发布 |
| 证据账本 / 人工更正 | `GET /agent/evidence-ledgers/:run_id`、`POST .../claims/:claim_id/corrections`;Claim 状态与 revision 链 |
| 深入研究 (实验) | `POST .../messages/agent` `mode=research`:受限 Planner 循环 MaxSteps 8 / MaxReplans 2,白名单含 `investigate_visual`(在已定位时间窗内按硬预算读取原始帧) |
| 证据漏斗 (实验) | `mode=evidence_funnel` 固定八步,Planner 只在有限候选中选择;视觉确认只读已持久化 OCR/视觉 observation |
| 引用回放 | 引用携带 modality / 毫秒范围 / anchor quote / source refs;`GET /media/task/:id/playback` 与 `/timeline` 支撑跳转和时间轴 |
| 转写状态机 | 分片进度、worker 并发、重试预算、已完成分片复用(mq.asr_* 配置) |

## 已知演绎(原型与真实实现的差异)

- **流式节奏**:Agent 真实实现是整段生成后按约 80 字分片发送;原型为演示体验按更小粒度逐字流出。
- **漏斗 / 研究模式的实时进度**:两者当前均为非流式接口;原型以模拟的实时步骤呈现,接真实后端时需要轮询或等待流式化。
- **strict_rag 的执行过程面板**:真实 SSE 没有检索中间事件,面板中的检索步骤由前端推断展示(与现网聊天页对普通 RAG 的处理一致),UI 内已明确标注。
- **账本按钮只出现在 Agent / 研究 / 漏斗路径**:标准 RAG 不写 Claim/Evidence,原型已按此对齐。
- **检索命中卡的时间列**:SSE 的 `retrieve_hits` 只带 chunk_index / score / video_title,时间列为原型补充的演示信息。
- **知识库范围的 Agent / 研究 / 漏斗**:后端直接拒绝,UI 以禁用态呈现,不做假象。
