# 摘要生成的启用与回退

两个进程级开关兼容默认启用。它们控制新的 v2 受理和可选视觉动作，不改变任务/文档权限、已存内容的读取或旧客户端的手动导入语义。

在配置示例中增加以下小节；不要为切换功能修改数据库连接、密钥或队列：

```yaml
summary_experience:
  v2_generation_enabled: true
  visual_enrichment_enabled: true
```

环境变量覆盖 YAML：

```sh
VIDLENS_SUMMARY_V2_GENERATION_ENABLED=false
VIDLENS_SUMMARY_VISUAL_ENRICHMENT_ENABLED=false
```

变量必须为合法布尔值；无效值使配置加载失败。修改配置后按正常流程重启进程。读兼容、worker 注册和迁移应先于接受新请求；混合新旧 worker 的实际发布流程须另行验收。

## 停止新生成

关闭 `v2_generation_enabled` 后，新自动 URL 导入、本地自动合并导入以及 active-source 手动生成/重新生成、文字来源刷新/v2 强制 ASR 或继续、单独补图重试返回 HTTP 503 `summary_generation_disabled`，不冻结新 profile、不创建任务/generation、不发布队列消息。已经受理的相同 Idempotency-Key 重放仍返回原始受理回执；请求内容变化仍返回 409。

已经受理的冻结 generation 可继续下载/来源/ASR/摘要和重试直至完成；开关不取消它们或换 profile。若需要停止某个在途任务，应使用原有明确取消/删除流程和 lease/source 围栏，不能把部署开关当作取消指令。旧手动上传、旧 summary API 路径与旧/新已发布结果、用户版本、来源、截图、标签和历史仍可读取；不将旧入口自动转换成付费 v2 生成。

## 停止补图

关闭 `visual_enrichment_enabled` 后，新的单独补图重试返回 422 `visual_disabled`，不接受新预算、不创建 attempt、不入队。相同已受理 Idempotency-Key 的回执重放仍允许。已受理的文字生成仍完成。即使其冻结选项请求了视觉，视觉 hook 也不调用底层抽帧/VLM 服务，终态明确 `visual_state=skipped`、`fallback_reason=visual_disabled`，已发布文字保留，resolved mode 为 text。未请求视觉的任务继续是 not_requested。已受理的 visual-only retry 同样诚实结束为 skipped/visual_disabled，并逐字保留原 generated 行和用户版本；它不重跑文字、ASR、下载或标签。这个开关不隐藏已登记的历史图片。

进程重启前已经开始的外部视觉调用无法由另一个进程的配置撤销；应先让旧进程完成或按正常取消/优雅停机处理，在新的进程中恢复并遵守原有检查点/预算。开关不重置预算、不批量重跑，不删除表或用户内容。

## 单独提交清单

以下仅是部署开关的后续改动；不需要重新暂存其他 worker/RAG/UI WIP。`wiring.go` 和两个 admission 文件只包含这里新增的接点，主任务应在既有 HTTP/worker 提交完成后暂存对应差异。

```text
internal/config/config.go
internal/config/loader.go
internal/config/summary_experience.go
internal/config/summary_experience_test.go
internal/service/summary_rollout.go
internal/service/summary_rollout_test.go
internal/service/media_import.go
internal/service/media_summary_generation.go
internal/mq/summary_rollout_test.go
cmd/server/wiring.go
docs/operations/summary-rollout.md
```

验证覆盖默认/YAML/env/错误配置、新受理拒绝、已受理重放、旧入口、旧正文与历史、已受理文字继续完成/视觉零调用，以及 consumer 同一 generation 完成与重投不重复。实际部署切换、混合版本消费者和回退实操仍需环境验收。

单独补图的 `authorize_new_visual_budget=true` 只随用户明确点击的新请求提交。部署开关、重新连接、队列重投与进程恢复均不生成新 attempt，也不清空计数或延长原接受时的执行截止时间。切换 v2 admission 后，已经接受的 visual-only attempt 可继续完成；切换 visual gate 后，它保留原文并跳过视觉调用。完整请求/读取约定见 [summary generation contract](../architecture/summary-generation-contract.md#visual-only-retry)。


文字来源刷新与 v2 `transcribe?force=1` / 失败后的显式继续属于新受理动作，关闭 v2 admission 时返回 503；同 key 的原受理回执仍可重放。已经受理的 source-refresh 来源/ASR/摘要队列工作继续使用冻结身份，恢复不换 profile、不自动升级旧手动任务。候选失败保留原来源、完整文字投影、摘要与用户版本。视觉开关只影响实际可选补图，既不阻止字幕/ASR 来源发布，也不要求重算保留的同源正文。详细请求与空事件页恢复约定见 [来源刷新契约](../architecture/summary-generation-contract.md#refresh-or-continue-the-text-source)。
