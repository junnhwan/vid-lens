# 聊天前端与后端边界

`frontend/components/chat/ChatWorkspace.tsx` 是正式 Chat / Agent 工作区。视频与知识库都提供 Chat 和 Agent；`useConversationSession` 统一会话加载、发送、取消、消息更新和终态处理。

## 当前执行路径

- Chat 使用标准检索链路，通过 SSE 返回回答和 citations。
- Agent 提供同步和 SSE 两种接口，共用服务端 Planner/Tool/Observe 循环。
- Agent 流式请求消费真实后端进度事件，消息内 ThinkingProcess 时间轴展示公开规划、工具步骤、简要决策说明和 provider reasoning。
- 正文和 reasoning 分通道，以 32 毫秒批次更新；完成或错误事件到来前先刷新缓冲。
- `done.answer` 覆盖累计正文，最终 citations 以服务端保存结果为准。

## 会话状态

用户向上滚动时暂停自动跟随。取消、断流和失败结束运行中节点；请求回调必须检查当前会话身份，不能更新已经切换的会话。reasoning 仅在当前实时页面展示，成功 Agent 的公开规划摘要与工具步骤可在会话回放中查看。

会话快照与右栏复用同一执行轨迹。快照提供版本、run、mode、steps、citations、memory identity 和 policy；Run/Step/ToolCall 是执行状态与恢复的权威来源。记忆设置由 `MemorySection` 和 memory API 提供。

## 已实现的产品能力

- 单视频 Chat / Agent：回答、引用详情、视频时间跳转和播放回放。
- 知识库 Chat / Agent：在授权成员任务集合内进行跨视频检索和回答。
- 多模态时间线：展示转写、OCR、画面描述和对应时间来源。
- Agent 运行轨迹：展示规划摘要、工具开始/结果、步骤终态、回答和 citations。
- 运行诊断：展示分视频证据、工具预算、运行状态和会话记忆策略。
- 检索测试台：支持 hybrid、vector、keyword 模式和阶段结果。

## 当前产品边界

- `investigate_visual` 必须使用服务端确认的 seed windows；前端不能提交无范围约束的全片视觉调查。
- Agent 由 HTTP 同步或 SSE 请求驱动；服务端执行状态通过 Run 查询接口读取。
- 前端展示的检索过程以真实 SSE 事件和服务端保存的步骤为准；未由后端发送的过程信息只能作为 UI 状态提示。
- 取消或失败时可以展示已收到的部分文本，但只有收到 `done` 且保存成功的回答才进入成功消息状态。

## 原型联调边界

原型页面使用本地演示数据和模拟时间节奏；正式页面使用 API 返回的任务、会话、引用和 Agent 运行状态。原型播放器使用本地视频，正式播放器使用对象存储流地址和 Range 请求。两者共享交互结构，但生产行为以服务端作用域、工具白名单、预算、租约和状态机为准。

回放链接携带毫秒参数，播放器在元数据就绪后定位；知识库证据按视频分组展示，引用详情读取 citation 的模态、时间范围和 source refs。
