package mq

import (
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

// Producer 任务生产者
type Producer struct {
	client *asynq.Client
}

// NewProducer 创建任务生产者
func NewProducer(redisAddr string) (*Producer, error) {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return &Producer{client: client}, nil
}

// EnqueueAnalyze 投递视频分析任务
// 面试亮点：投递即返回，接口 RT 压缩到 50ms 以内
func (p *Producer) EnqueueAnalyze(taskID int64, md5 string) error {
	payload, _ := json.Marshal(AnalyzePayload{
		TaskID: taskID,
		MD5:    md5,
	})

	task := asynq.NewTask(TaskTypeAnalyze, payload)
	_, err := p.client.Enqueue(task,
		asynq.MaxRetry(3),              // 最多重试 3 次
		asynq.Queue("critical"),        // 高优先级队列
		asynq.TaskID(fmt.Sprintf("analyze_%s", md5)), // 幂等 Key
	)
	return err
}

// EnqueueTranscribe 投递文字提取任务
func (p *Producer) EnqueueTranscribe(taskID int64, md5 string) error {
	payload, _ := json.Marshal(AnalyzePayload{
		TaskID: taskID,
		MD5:    md5,
	})

	task := asynq.NewTask(TaskTypeTranscribe, payload)
	_, err := p.client.Enqueue(task,
		asynq.MaxRetry(3),
		asynq.Queue("default"),
		asynq.TaskID(fmt.Sprintf("transcribe_%s", md5)),
	)
	return err
}

// Close 关闭生产者连接
func (p *Producer) Close() error {
	return p.client.Close()
}
