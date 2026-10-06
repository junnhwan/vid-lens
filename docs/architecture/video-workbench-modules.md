# 视频工作台与转写职责

视频详情页组合视图和确认文案。`useVideoWorkbenchData` 使用现有 React Query，按登录身份和 task ID 隔离任务、来源、签名、索引、进度与成果查询，消费 AbortSignal；处理状态决定轮询，发布变化刷新已有证据。切换身份/视频会重新挂载本地交互 owner，旧响应留在旧查询身份中。

`useVideoActions` 复用服务端 action 准入和现有预检，只提交转写、对齐、摘要、索引、画面设置及下载等既定操作，并刷新对应查询。提交槽阻止尚未完成的重复请求。`useVideoPlayback` 统一 seek、高亮时钟、阅读位置与播放暂停衔接；签名刷新来自数据 Module。`TranscriptPanel`、`VisualEvidencePanel`、`RetrievalIndexPanel` 管理阅读展开、悬停等局部交互，不调用模型。进度面板接受工作台集中查询，其他页面仍可使用原独立容器。

`internal/mq/transcription_workflow.go` 集中分窗、识别重试/已完成窗口复用、原文装配和租约保护的发布；`transcript_alignment.go` 的工作流检查对齐只改变 TimedSegments，再原子发布来源时间、正文与索引失效。纯装配和实际 `transcript.Aligner` Adapter 保留在 `internal/transcript`。MQ 消费器继续拥有消息、heartbeat、阶段交接与终态；传给工作流的租约写入和阶段 callback 是调度边界，没有为每个辅助函数创建 Interface。

迁移保持既有分窗、识别、文本接缝和来源 ID 算法；已有分片复用、发布回滚、alignment-only 不重复 ASR、消息交接与终态测试作为行为基线。新增前端取消/迟到响应和重复提交回归。跨平台安装/自检说明见 [可选运行依赖](../optional-runtime.md)。
