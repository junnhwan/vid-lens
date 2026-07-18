package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vid-lens/internal/model"
	"vid-lens/internal/observability"

	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

func groupReaderConfig(brokers []string, topic, groupID string) kafka.ReaderConfig {
	return kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1e3,
		MaxBytes:       1e6,
		CommitInterval: 0,
		ReadBackoffMin: 100 * time.Millisecond,
		ReadBackoffMax: time.Second,
	}
}

func (c *Consumer) readerFactory() kafkaReaderFactory {
	if c.newKafkaReader != nil {
		return c.newKafkaReader
	}
	return func(config kafka.ReaderConfig) kafkaMessageReader {
		return kafka.NewReader(config)
	}
}

func (c *Consumer) restartBackoff() time.Duration {
	if c.readerRestartBackoff > 0 {
		return c.readerRestartBackoff
	}
	return time.Second
}

func consumeReader(ctx context.Context, reader kafkaMessageReader, handler kafkaMessageHandler) (err error) {
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("关闭 Kafka reader 失败: %w", closeErr))
		}
	}()

	for {
		message, fetchErr := reader.FetchMessage(ctx)
		if fetchErr != nil {
			return fmt.Errorf("获取 Kafka 消息失败: %w", fetchErr)
		}
		if handleErr := handler(ctx, message); handleErr != nil {
			return fmt.Errorf("处理 Kafka 消息失败: %w", handleErr)
		}
		if commitErr := reader.CommitMessages(ctx, message); commitErr != nil {
			return fmt.Errorf("提交 Kafka offset 失败: %w", commitErr)
		}
	}
}

func (c *Consumer) runGroupConsumer(ctx context.Context, name string, config kafka.ReaderConfig, handler kafkaMessageHandler) {
	for ctx.Err() == nil {
		reader := c.readerFactory()(config)
		observability.Log(ctx, slog.Default(), slog.LevelInfo, "kafka consumer started", slog.String("consumer", name), slog.String("topic", config.Topic), slog.String("group", config.GroupID))
		err := consumeReader(ctx, reader, handler)
		if ctx.Err() != nil {
			return
		}
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "kafka consumer rebuilding reader", slog.String("consumer", name), slog.String("error", observability.SafeError(err)))

		timer := time.NewTimer(c.restartBackoff())
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

// Group consumers use FetchMessage and explicitly commit only after the handler
// either completes the business operation or durably records its failure for RetryScheduler.
// Any fetch, handler, or commit error closes this reader; the outer loop rebuilds it after backoff.
func (c *Consumer) poisonAwareHandler(name, groupID string, handler kafkaMessageHandler) kafkaMessageHandler {
	return func(ctx context.Context, message kafka.Message) error {
		err := handler(ctx, message)
		if err == nil || !isPoisonMessageError(err) {
			return err
		}
		if c == nil || c.repo == nil || c.repo.TaskMessageFailure == nil {
			return fmt.Errorf("poison 消息隔离仓储未初始化: %w", err)
		}
		failure := &model.KafkaMessageFailure{
			ConsumerGroup: groupID, ConsumerName: name, Topic: message.Topic,
			Partition: message.Partition, MessageOffset: message.Offset,
			MessageKey: append([]byte(nil), message.Key...), Payload: append([]byte(nil), message.Value...),
			ErrorMessage: truncateError(err),
		}
		if persistErr := c.repo.TaskMessageFailure.Record(failure); persistErr != nil {
			return fmt.Errorf("持久化 poison 消息失败: %w", persistErr)
		}
		return nil
	}
}

func isPoisonMessageError(err error) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &typeErr)
}

func (c *Consumer) startGroupConsumer(ctx context.Context, name string, brokers []string, topic, groupID string, handler kafkaMessageHandler) {
	durableHandler := c.poisonAwareHandler(name, groupID, handler)
	observedHandler := func(ctx context.Context, message kafka.Message) error {
		startedAt := time.Now()
		err := durableHandler(ctx, message)
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.ObserveKafkaJob(name, time.Since(startedAt))
		}
		return err
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runGroupConsumer(ctx, name, groupReaderConfig(brokers, topic, groupID), observedHandler)
	}()
}

// Wait blocks until all started group consumers have stopped.
func (c *Consumer) Wait() {
	c.wg.Wait()
}

func (c *Consumer) StartAnalyzeConsumer(ctx context.Context, brokers []string, topic, groupID string) {
	c.startGroupConsumer(ctx, "analyze", brokers, topic, groupID, c.handleAnalyze)
}

func (c *Consumer) StartTranscribeConsumer(ctx context.Context, brokers []string, topic, groupID string) {
	c.startGroupConsumer(ctx, "transcribe", brokers, topic, groupID, c.handleTranscribe)
}

func (c *Consumer) StartDownloadConsumer(ctx context.Context, brokers []string, topic, groupID string) {
	c.startGroupConsumer(ctx, "download", brokers, topic, groupID, c.handleDownload)
}

func (c *Consumer) StartRAGIndexConsumer(ctx context.Context, brokers []string, topic, groupID string) {
	c.startGroupConsumer(ctx, "rag_index", brokers, topic, groupID, c.handleRAGIndex)
}
