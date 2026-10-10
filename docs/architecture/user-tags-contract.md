# 用户标签后端契约

2026-10-10：S4 本地后端已接入实际路由与数据库；SQLite 和隔离 PostgreSQL 覆盖命名空间、并发创建、关系版本 CAS、拒绝/恢复、合并、分页前 all/any 筛选。前端已接真实视频标签、建议接受/拒绝/恢复、词表创建/改名/别名/显式合并，以及视频库 all/any 服务端分页筛选；新增本地 UI 回归通过。真实 LLM 标签质量和浏览器端到端体验仍单独验收。

## 公开 API

所有路径带现有 `/api/v1` 前缀、登录身份与资源归属校验。Demo 角色不能写入。

| 方法/路径 | 参数 |
| --- | --- |
| GET /tags | search、page、page_size、sort=name/usage（缺省 name） |
| POST /tags | name |
| PATCH /tags/:tag_id | name、expected_version |
| PUT /tags/:tag_id/aliases | aliases、expected_version |
| POST /tags/:tag_id/merge | target_id、source_version、target_version；Idempotency-Key |
| GET /media/task/:id/tags | 返回 version、assignments、suggestions、decisions、classification? |
| PATCH /media/task/:id/tags | expected_version、add_ids、remove_ids、keep_auto_ids |
| POST /media/task/:id/tag-suggestions/:suggestion_id/decision | decision=accept/reject/restore、expected_version |
| GET /media/list | 原查询条件外增加 tag_ids（逗号分隔）及 tag_match=all/any，默认 all |

名称以 Unicode NFKC、空白折叠和 case fold 归一；保留 C++、C#、.NET 的技术标点。标签和确认别名共用单一 owner 命名空间。上限：名称 80 Unicode 字符、别名 20、筛选 ID 50、单批候选 5、视频关系 20。显式空 tag_ids、空条目、重复查询参数与未知 match 返回 400；外用户 ID 返回 404；版本或命名空间冲突返回 409。筛选发生于数据库分页和总数之前；零匹配不会回退整库。标签列表数量只统计当前用户可访问的视频。`sort=usage` 在全词表数据库计数排序后分页，默认展示最多 6 个常用标签，不从已分页的字母列表猜测全库热度。

手工新增/保留自动标签会保护标签并转成 manual 关系。移除/拒绝写入持久拒绝；重新生成不会恢复被拒绝关系。restore 仅清除拒绝，accept 才建立关系。自动分类保留 manual 关系，只替换 auto 关系；个人意图标签不自动推断。严格名称或确认别名自动复用；跨实体合并仅经显式 merge 路由，不向候选配方开放。

## 生成任务集成

`tx.UserTag.PrepareTagIntent` 在摘要发布事务中冻结 owner/task/sourceDigest/generation/generatedVersion/expectedTagVersion/enabled/candidates。`PublishPendingCandidates` 在提交后、现有 TaskJob processing lease 内消费冻结 payload，并原子写标签、分类 receipt 和 completed 意图。job generation、source digest、generated version、tag version、实际 generation AgentRun 的取消状态及租约都受校验；没有第二套 workflow lease。标签失败使用 `MarkTagIntentFailed` 保存 failed/cancelled 状态，正文仍已就绪。后台重试必须重新取得当前 summary job 的合法 processing lease；不能绕过版本冲突恢复。

基础 `PublishCandidates` 为已有无租约 fixture/受信内部调用保留入口；带 LeaseToken 则强制检查实际 job/run。生产 durable intent 消费不可省略 token。receipt 防止相同生成的重投覆盖后续手工决定；候选发生变化返回 idempotency_conflict。

`classification` 仅投影该视频当前 generation 的真实 durable intent，包含 status、enabled、error_code、generated_version；不公开候选 JSON 或内部路径。前端 pending 时短轮询既有标签 GET，completed 后停止；failed 显示“标签处理未完成，摘要正文仍可阅读”，不把正文判失败。来源变化的旧 pending 在读取时投影 cancelled，不能无限等待旧来源。
