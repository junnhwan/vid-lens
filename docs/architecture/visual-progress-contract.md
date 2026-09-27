# 视觉处理进度契约

`GET /api/v1/media/task/:id/visual-progress` 使用现有 JWT 认证，并按视频 owner 隔离。成功响应沿用 `{ "code": 200, "message": "success", "data": ... }`；不存在或非 owner 返回 404。该接口只读，不含 OCR、Vision 文本、对象路径或内部 processing token。

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "task_id": 42,
    "attempt_id": "va_6f52e8c14cc068a1",
    "status": "running",
    "phase": "observing_frames",
    "total_frames": 24,
    "processed_frames": 7,
    "failed_frames": 1,
    "ocr_failed_frames": 1,
    "vision_failed_frames": 0,
    "started_at": "2026-09-27T10:00:00Z",
    "updated_at": "2026-09-27T10:01:02Z"
  }
}
```

`status` 可以为 `not_started`、`waiting_to_start`、`queued`、`running`、`completed`、`skipped`、`failed`、`canceled`、`interrupted`。`phase` 为 `waiting_for_slot`、`provider_check`、`downloading`、`extracting`、`observing_frames`、`publishing`、`published`，或初始占位 `not_started` / `waiting_for_worker` / `disabled`。`completed` 表示本次帧批次已原子发布；单帧 OCR、Vision 或上传失败仍由失败计数表示。`skipped` 的 `error_code=no_visual_provider` 表示该处理环境没有可用的 Vision 配置和 OCR 命令；`visual_disabled` 表示用户关闭了该视频的自动画面分析。

提取帧完成前，`total_frames` 为 `null`，前端应显示阶段和已处理帧数，不显示百分比。开始新处理尝试时，`attempt_id` 变化，计数从零开始；等待新 worker 时没有 `attempt_id`。旧 worker 的迟到进度或发布不会更新新尝试。刷新页面后重新读取此接口即可恢复当前进度视图。`interrupted` 表示处理尝试已失去 task lease 或任务失败；它不代表帧级断点可复用。

例如提取期间返回 `{"status":"running","phase":"extracting","total_frames":null,"processed_frames":0,"failed_frames":0}`；下一次读取进入 `observing_frames` 后总量才可能为具体整数。前端应以当前 `attempt_id` 为界丢弃前一次请求的迟到响应。

前端可以按以下方式读取并显示：

```ts
type VisualProgress = {
  attempt_id?: string;
  status: string;
  phase: string;
  total_frames: number | null;
  processed_frames: number;
  failed_frames: number;
};

const response = await fetch(`/api/v1/media/task/${taskId}/visual-progress`, {
  headers: { Authorization: `Bearer ${token}` },
});
if (!response.ok) throw new Error('视觉进度读取失败');
const { data } = (await response.json()) as { data: VisualProgress };
const detail = data.total_frames === null
  ? `已处理 ${data.processed_frames} 帧` // 总量未知，无百分比
  : `已处理 ${data.processed_frames}/${data.total_frames} 帧`;
```

进度保存在 `video_visual_progress`，与稳定证据表 `video_visual_frames` 分离。视觉分支在转写任务的 processing lease 下运行，逐帧写进度；所有帧处理完毕才在同一数据库事务中替换证据并标记 `completed`。帧对象路径包含尝试标识，因此未发布的重试不会覆盖旧证据对象。当前不提供帧级断点恢复，也不承诺清理崩溃后未发布的孤立对象；这是后续对象回收工作。
