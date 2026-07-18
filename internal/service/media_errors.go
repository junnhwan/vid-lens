package service

import "errors"

var (
	ErrTaskNotFound            = errors.New("任务不存在")
	ErrTaskForbidden           = errors.New("无权删除此任务")
	ErrTaskCleanupUnavailable  = errors.New("任务清理服务未初始化")
	ErrTaskDispatchUnavailable = errors.New("系统繁忙，请稍后重试")

	// ErrTaskActive indicates that queued/running work must be canceled before
	// its resources can be deleted. VidLens does not yet implement a worker
	// tombstone protocol, so rejecting the operation is safer than late writes.
	ErrTaskActive = errors.New("任务正在处理中，暂不支持删除")
)
