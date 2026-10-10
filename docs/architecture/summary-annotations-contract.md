# 摘要选段问答契约

2026-10-10 后端实际接入普通问答、普通 SSE、Agent 同步及 Agent SSE 的 `context_refs`；新增 SQLite、隔离 PostgreSQL、race 以及 Agent 冻结/恢复回归已通过。前端已接发送前附件栏、移除、生成/修订版本及 nullable 历史快照展示；40 项聊天/标签/修订/库相关本地 UI 回归通过，真实模型质量与浏览器端到端仍单独验收。

`GET /media/task/:id/summary/blocks/:block_id/context?version_ref=generated:1`，或 `version_ref=revision:<revision_id>`，返回 `canonical_text`、`document_digest`、`block_digest`、`version_ref`、`source_refs`、`figures`、标题。摘要 GET 增加当前有效 `version_ref`。逻辑 ID 为 `summary-title`、`summary-overview`；legacy Markdown 的临时段落 ID 为 `legacy-0` 起的双换行段落编号，不调用模型迁移。

```json
{"question":"这个配置怎么设置？","context_refs":[{"kind":"summary_selection","task_id":42,"version_ref":{"generated_version":1},"document_digest":"...","block_id":"config","block_digest":"...","text_start":2,"text_end":10,"quote":"实际节选"}]}
```

范围按服务端 `canonical_text` 的 Unicode code point、半开区间计算。Goldmark AST 投影保留可见文字、链接文字、代码字面量及图注，不包含链接目标或 HTML。中文/emoji 和转义字符有往返回归。最多 3 个附件，总计 3000 Unicode 字符；问题仍单独受原 1000 字符限制。图片选择 `kind=summary_screenshot` 还需当前块真实绑定的 `screenshot_ref`；限额计入冻结图注和 alt。任意 URL 或未注册/撤销截图不能成为附件。

发送前验证登录会话、owner、scope、来源、版本、文档与块摘要值、范围和 quote。仅有旧 generated 号而当前版本已变，返回 409 `summary_selection_stale` 要求重新选择；有权限的保存 revision 可读取。明确错误包括：400 `invalid_version_ref`/`invalid_context_ref`/`invalid_selection_range`/`summary_selection_limit`，409 `summary_quote_mismatch`，403 `context_outside_scope`，404 `not_found`。流式准备错误发生于首次 SSE 前，保留对应 HTTP 状态。

用户消息的可空 `context_annotations_json` 为服务端验证后快照，保存实际 quote、版本/摘要值、块、来源引用和 provenance。当前文字与图注属于 `derived_summary`，不是视频原话，不能因此产生 [Cn] 原视频引用或授予工具权限。已注册截图还保存实际 observation_id/capture_ms 与冻结 caption/alt；`input_mode=caption_only` 表明本轮只输入图注与元数据，没有输入原图像素。没有对象存储路径、签名 URL 或图片上传平台。

Agent 的同一冻结快照保存在原 PolicySnapshot，重投/恢复使用它；新的附件不可以替换已接受执行。保存消息时再次在原事务校验访问；历史提示词按当前权限/范围校验后使用旧快照，不回读最新摘要。最近最多 6 条消息的注释总预算 6000 字符，超限返回 422 `annotation_history_limit`，不可访问返回 409 `annotation_history_inaccessible`。注释与问题共同进入现有模型调用/Agent token 预算。普通问答不写回摘要。

显式修订仍走原 edit-runs。新增 `selected_block_ids` 最多 50 个，在操作与运行策略中冻结。v2 选块范围仅更新已有正文/标题/图注，标题与概览映射对应逻辑操作；范围内不增删/移动结构。legacy 选块限制精确 byte anchor 位于对应 Markdown 原段。planner 和 repository 的 preview/apply 都验证范围，越界返回 422 `edit_scope_violation`；全文修订保留已有结构能力。请求可带 `expected_content_digest` 和 `expected_version_ref`，两者进入幂等请求哈希，并在 Begin 的 task lock 事务中核对当前有效正文；旧 generated 选择即使人工 revision 仍为 0、block ID 相同或正文相同，也不能越过 generated version 变更。未带这些字段的既有调用仍兼容。

活动沿用原 Agent progress/step/tool 和修订操作轮询。Planner 可输出安全 `public_title`（最多 40 字），历史执行也保存该字段；缺失、不安全或动作不符时按实际工具显示；public_summary 的生产截断与提示词上限统一为 120 字。修订 `activities` 只投影真实持久 planner/术语修正步骤和耗时，没有预设检索/截图活动。`proposed` 为等待用户应用的终态，apply/undo 分开执行。
