package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"vid-lens/internal/model"
)

// contentDedupHits 统计内容+目标级去重的命中次数（spec 03 第 10 行：去重命中
// 的计数可观测，为简历"省 N 次 AI 调用"提供可跑统计来源）。每次重复上传/
// 重复请求命中已有成功结果（秒传到 Completed、不重跑 AI）即 +1。
// 这是进程内计数器（非持久），长期运行后需接入 metrics 落盘；本 spec 只保证
// 计数可观测、验收命令能跑出真实数字（spec 第 136 行"不许估算"）。
var contentDedupHits int64

// ContentDedupHits 返回当前内容去重命中次数（用于验收命令与可观测性）。
func ContentDedupHits() int64 { return atomic.LoadInt64(&contentDedupHits) }

// recordContentDedupHit 记录一次内容去重命中。命中 = 等价省一次对应 job_type
// 的 AI 调用（ASR / 摘要 LLM / 索引 embed），故"省 AI 调用次数 = 命中次数"
// （spec 第 128 行派生结论）。
func recordContentDedupHit() { atomic.AddInt64(&contentDedupHits, 1) }

// resetContentDedupHitsForTest 重置命中计数器到 0（仅测试用，隔离用例）。
func resetContentDedupHitsForTest() { atomic.StoreInt64(&contentDedupHits, 0) }


// 内容级 + 分析目标级去重（spec 03）。
//
// 三层幂等分工（写进注释，面试最可能被追的点）：
//  1. 文件层（Asset file_md5 唯一索引 + FindByMD5 + CreateOrRestore）：
//     复用资产对象，省带宽。同一内容不重传 MinIO。本 spec 不碰。
//  2. 内容+目标层（本文件）：复用分析结果，省 AI token。同一 (file_md5,
//     job_type) 已有成功结果 → 复用，不重跑 AI。Redis SETNX 防并发抢占
//     （短期），DB 唯一约束 (file_md5[, embedding_model]) 持久兜底（长期）。
//  3. MQ 消息层（spec 02，internal/mq/idempotency.go 的 SETNX on MessageId）：
//     去重重复投递，保证 at-least-once 无副作用。
//
// 三层正交，各填各的窗口：文件层填"同内容重传"窗口、内容+目标层填"同内容
// 重跑 AI"窗口、MQ 层填"同消息重投"窗口。本层与 MQ 层不可合并——MQ 层按
// MessageId 去重，重复投递同一消息；本层按 (file_md5, job_type) 去重，不同
// 消息、不同 task 间的同内容同目标重复请求。两者粒度不同。
//
// 索引去重特别注意：(file_md5, embedding_model) 而非复用 transcription。
// 索引重建（分块/embedding 模型变更）后旧索引 status 被改写，不再挡住重索引
// （spec 第 66 行）。索引去重因依赖 embedding_model，落在转写消费侧
// indexAfterTranscription（该处可解析用户默认 embedding 模型）；上传侧
// createTaskFromAsset 只判定转写+摘要（不依赖 embedding 模型）。

const (
	// contentDedupLockTTL 是内容+目标级 Redis SETNX 锁的 TTL：处理时长 + guard。
	// 与 spec 02 消息级幂等同一思想（lease + guard），但键不同：
	//   mq:dedup:content:<file_md5>:<job_type>
	// 一个请求跑 AI 期间其余并发请求拿不到锁 → 等待复用，避免同内容并发跑两遍。
	// 处理完成后键随 TTL 过期（不主动 Del），这样 crash 在 AI 完成与写结果之间
	// 仍能抑制并发重跑；TTL 兜底确保最终可被新请求重新获取。
	contentDedupLockTTL = 40 * time.Minute
)

// contentDedupKey 构造内容+目标级 SETNX 锁键。
func contentDedupKey(fileMD5, jobType string) string {
	return fmt.Sprintf("mq:dedup:content:%s:%s", fileMD5, jobType)
}

// contentDedupDecision 描述对一次上传按 (file_md5, 各 job_type) 的去重判定。
type contentDedupDecision struct {
	// HasTranscription / HasSummary：转写/摘要是否已有成功结果
	// （跨 task、跨用户，按 file_md5 查，行存在即 Completed 语义）。
	HasTranscription bool
	HasSummary       bool
	// ReuseFromTaskID：被复用的源 task id（结果行所属 task），用于结果引用关联。
	// 两个结果可能来自不同 task（部分命中场景），取第一个有结果的源 task。
	ReuseFromTaskID int64
}

// AllHit 是否转写+摘要全部命中。全命中 → 新 task 秒传到 Completed，不 enqueue。
// （索引去重单独判定，见消费侧 indexAfterTranscription。）
func (d contentDedupDecision) AllHit() bool {
	return d.HasTranscription && d.HasSummary
}

// lookupContentDedup 按 file_md5 查任意 task、任意用户的成功转写/摘要结果
// （spec 03 第 59 行）。只复用成功结果：转写/摘要表行存在即成功完成
// （这两表无 status 列，行存在 = Completed）。失败结果不会落行，故不会被复用
// （spec 第 14 行）。
func (s *MediaService) lookupContentDedup(fileMD5 string) (contentDedupDecision, error) {
	dec := contentDedupDecision{}
	if fileMD5 == "" {
		return dec, nil
	}
	tx, err := s.repo.Transcription.FindByMD5(fileMD5)
	if err != nil {
		return dec, fmt.Errorf("content dedup lookup transcription: %w", err)
	}
	if tx != nil {
		dec.HasTranscription = true
		dec.ReuseFromTaskID = tx.TaskID
	}
	summary, err := s.repo.Summary.FindByMD5(fileMD5)
	if err != nil {
		return dec, fmt.Errorf("content dedup lookup summary: %w", err)
	}
	if summary != nil {
		dec.HasSummary = true
		if dec.ReuseFromTaskID == 0 {
			dec.ReuseFromTaskID = summary.TaskID
		}
	}
	return dec, nil
}

// acquireContentDedupLock 获取内容+目标级 SETNX 锁。
// 返回 acquired=true 表示当前请求获得执行权；acquired=false 表示同内容同目标
// 已有请求在跑 AI，调用方应走复用路径（等待已有结果或直接命中）。Redis 不可用
// 时降级为 acquired=true（不阻塞主流程，DB 唯一约束兜底）。
func (s *MediaService) acquireContentDedupLock(ctx context.Context, fileMD5, jobType string) (acquired bool, err error) {
	if s.rdb == nil || fileMD5 == "" || jobType == "" {
		return true, nil
	}
	key := contentDedupKey(fileMD5, jobType)
	ok, err := s.rdb.SetNX(ctx, key, "1", contentDedupLockTTL).Result()
	if err != nil {
		// Redis 故障不破坏去重语义：放行执行，DB 唯一约束兜底。
		return true, nil
	}
	return ok, nil
}

// completeTaskFromDedupHit 在内容去重全命中时把新 task 秒传到 Completed。
// 不 enqueue 任何 job，结果行不复制——详情页按 file_md5 关联已有行展示
// （见 GetTaskDetail 的 file_md5 join 路径，spec 第 65 行）。
//
// 仅上传链路全命中（转写+摘要都有）调用；force=true 时不调用本路径
// （force 仍可强制重跑，spec 第 39 行）。
func (s *MediaService) completeTaskFromDedupHit(taskID int64) error {
	return s.repo.Task.UpdateStatusAndStage(taskID, model.TaskStatusCompleted, model.TaskStageNone, "")
}

// reuseResultByFileMD5 封装 RequestAnalysis/RequestTranscribe 共用的内容去重
// 短路范式：force=false 且当前 task 无自有结果行时，按 file_md5 查任意 task/
// 任意用户的成功结果。命中 → 抢 SETNX 内容锁、记命中、返回"已完成可直接查看
// 结果"（与原单 task 短路语义一致，不替 task 做整体完成判定——spec 第 60 行
// 分析目标级独立）。find 返回 (existing, error)；existing 非空即命中。
//
// 三层幂等分工见本文件顶部注释。此 helper 与文件层 Asset 秒传、MQ 消息级
// SETNX（spec 02）正交，不可合并到消息级层。
func (s *MediaService) reuseResultByFileMD5(
	ctx context.Context,
	task *model.VideoTask,
	jobType string,
	find func(fileMD5 string) (hasResult bool, err error),
) (hit bool, err error) {
	existing, lookupErr := find(task.FileMD5)
	if lookupErr != nil {
		return false, lookupErr
	}
	if !existing {
		return false, nil
	}
	if acquired, _ := s.acquireContentDedupLock(ctx, task.FileMD5, jobType); !acquired {
		return false, fmt.Errorf("任务正在处理中，请稍后查看结果")
	}
	recordContentDedupHit()
	return true, nil
}
