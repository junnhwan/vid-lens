# 聊天前端与后端边界

`frontend/components/chat/ChatWorkspace.tsx` 是正式 Chat / Agent 工作区。视频与知识库都提供 Chat 和 Agent。`useConversationSession` 统一会话加载、发送、取消、消息更新和终态处理。

Agent 仅走流式实时分支；research/funnel 选择器、专属等待流程、FunnelTrack 与 EvidenceLedger 已移除。Chat 和 Agent 都使用真实后端进度事件，消息内 ThinkingProcess 时间轴展示规划、工具、简要决策说明和模型接口提供的 reasoning。右栏复用同一轨迹。最终 done.answer 覆盖累计 token，引用使用最终 citations。

正文和 reasoning 分通道，以 32 毫秒批次更新，完成/错误事件到来前先刷新缓冲。用户向上滚动时暂停自动跟随。取消、断流和失败结束运行中节点；旧请求回调不得更新新会话。reasoning 仅实时展示，成功 Agent 历史回放公开规划摘要与工具步骤。Next 关闭默认压缩以避免 SSE 在响应结束后整段出现，详见 agent-streaming-contract.md。

历史模式名称仅保留在 `snapshotTraceAdapter` 和历史消息显示中。旧消息可以打开基础引用与时间跳转。记忆设置继续由 `MemorySection` 和 memory API 提供。

尚未扩展的能力：成员安全的知识库多轮内容注入、无 seed windows 的全片视觉搜索。取消与失败通过现有状态展示，不能把部分回答当作成功保存。

知识库研究页使用资料卡片展示来源和真实命中，右栏提供分视频证据、运行详情和会话记忆策略。检索测试台支持混合/向量/关键词模式和阶段结果。回放链接携带 t（毫秒），播放器在元数据就绪后定位。
