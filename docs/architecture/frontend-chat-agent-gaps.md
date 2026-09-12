# 聊天前端与后端边界

`frontend/components/chat/ChatWorkspace.tsx` 是正式 Chat / Agent 工作区。视频提供两种模式，知识库只提供 Chat。`useConversationSession` 统一会话加载、发送、取消、消息更新和终态处理。

Agent 仅走流式实时分支；research/funnel 选择器、专属等待流程、FunnelTrack 与 EvidenceLedger 已移除。普通 Chat 仍使用现有 SSE，右栏检索进度是前端推断；Agent 步骤来自真实工具事件。最终 done.answer 覆盖累计 token，引用使用最终 citations。

历史模式名称仅保留在 `snapshotTraceAdapter` 和历史消息显示中。旧消息可以打开基础引用与时间跳转。记忆设置继续由 `MemorySection` 和 memory API 提供。

尚未扩展的能力：知识库 Agent、成员安全的知识库多轮内容注入、无 seed windows 的全片视觉搜索。取消与失败通过现有状态展示，不能把部分回答当作成功保存。
