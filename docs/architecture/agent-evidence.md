# Agent 引用与视觉证据

Agent 的事实来源是当前用户授权视频中的转写、OCR、画面描述和带来源的视觉观察。引用保留 EvidenceID、视频、模态、来源与真实时间范围，时间未知时保持 unknown，不能从 chunk index 推算时间。

最终回答工具参数中的引用只是选择器。服务端按本轮已观察证据替换其正文与来源，并在 observation 边界再次 canonicalize。未观察、跨视频或伪造来源的引用不能进入最终生成。工具参数不能传入可信 memory context；记忆不是引用证据。

`search_visual_evidence` 检索已有 OCR/描述，`inspect_visual_window` 读取已存储窗口；只有 `investigate_visual` 会按已知 seed windows 下载视频、抽帧并调用 VLM。观察仍可能不充分或相互冲突，不代表事实已经被独立证明。`video_visual_observations`、帧存储及对象清理保持独立。

Inspector、Claim/Evidence 账本写入、人工更正 API 及 Ledger UI 已退役，回答不再做强制反例检索或像素复核。旧账本表从自动迁移注册移除，但不 DROP 既有表或数据。基础引用、EvidenceDrawer 和播放器时间跳转继续使用聊天快照与源证据。

发布与幂等见 [Agent 执行](agent-evolution.md)，事件与最终文本覆盖规则见 [SSE 契约](agent-streaming-contract.md)。
