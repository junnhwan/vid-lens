# 摘要体验实施进度

依据 2026-10-09 产品计划、实施指南和桌面 v2 原型实施。目标包含 M0、S0、S1、S2、S3、S4、S5A、S6；S5B 后置。本文是实际交付记录，不能替代各工作包的退出条件。

## 基线与工作约束

- 基线提交：`881a13ca0df28be57e0357a64daada58dfec065a`。
- 开发分支：`codex/summary-experience`；按完整能力本地提交，不推送、合并或部署。
- 保留原有 WIP：`docker-compose.yml`、`frontend/package.json`、`frontend/package-lock.json`。不纳入本次提交。
- 本机 Go 1.26.5，Node 24.19.0，yt-dlp 2026.08.19；已提供本地 PostgreSQL、Redis、MinIO 和 RabbitMQ 容器。容器存在不代表整条业务流程验收通过。
- PostgreSQL 验证使用独立 `vidlens_summary_test` 数据库，每个测试创建并删除临时 schema，不修改应用数据库。

## 已交付能力

### 统一来源和摘要文档的纯契约

`internal/textsource` 定义不可变媒体身份、cue、原始 cue 映射、文字 hash 和包含时间/身份的来源 digest。SRT 解析有大小、数量和时间限制，保留真实重复发言与重叠；字幕类型无证据时标 unknown，不变造 ASR 分片。

`internal/summarydoc` 定义同一份 v2 摘要文档、严格解析、来源/时间/图片登记验证、规范 JSON/hash、Markdown 投影和有界结构化补丁。正文和图注修订由同一文档派生；图片只接受服务端已登记且检查过的引用。保留代码中的字面文本，拒绝实际渲染的私有地址/未授权图片引用。

验证：两个包的单元测试、race 和 vet 通过。包含重复键/尾随 JSON、来源时间不符、重复/保留块 ID、未经登记的图片、补丁及投影等回归；均为本地测试，未使用真实视频或模型。

本地提交：`f4c5c3d`。

### B 站身份、字幕和下载适配

`internal/pkg/ytdlp` 新增普通 BV/经元数据转换的 AV/受限 b23 解析、结构化字幕探测/选择/获取和带身份验证的下载。保留合法的分 P 参数，拒绝错误/冲突/越界参数。下载核对实际 BV/分 P 元数据及下载前后官方 CID 映射；可取得所选媒体 URL 的 CID 时进一步核对。字幕列表排除弹幕；缺少类型证据时保留 unknown。

`internal/pkg/remoteurl` 为新 HTTP 请求逐跳校验目标，并拨号至同一个已验证公网 IP，限制响应、超时、重定向和 Cookie 作用域。新身份探测不支持普通代理绕过目标 IP 校验，会明确返回 `ErrHTTPProxyUnsupported`；外部 yt-dlp 的出站限制仍需部署环境保障。详细边界见 `internal/pkg/ytdlp/README.md`。

验证：适配包 race 和 vet 通过，覆盖解析/重定向/身份不符/选轨/下载元数据/临时文件清理；旧 URL 服务与 MQ 安全测试已调整假 DNS 和合法 p=1 预期。全部为 fixture 和本地 HTTP 测试，没有真实平台下载证据。上层自动作业接力另行验证。

## 正在集成，尚未完整交付

- S0：模型迁移、原子来源/摘要发布、来源读取和旧版本修订兼容；基础 SQLite/PostgreSQL 测试已通过，待完整服务回归和契约收拢。
- S1：B 站身份/分 P/字幕适配已通过 fixture 验证；持久接力、导入选项、请求幂等和 ASR 接力正在接入。
- S2：结构化生成配方、冻结配置、运行活动与生成状态待完成。
- S3、S4、S5A、S6：依赖上述契约继续实施；标签后端可独立推进。

真实 B 站、真实模型质量、授权浏览器流程和桌面体验尚未验收。Goal 保持 active，以上测试不能作为整项完成证据。
