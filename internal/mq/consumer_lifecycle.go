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

	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
)

// errStaleDispatch is returned by consumers when the message's dispatch lease no
// longer matches the task (the task was re-dispatched with a newer lease). The
// dedup handler treats it specially: release the idempotency key and Ack the
// stale message as a no-op, so the RetryScheduler's fresh dispatch (same
// MessageId) is not suppressed into a dead Queued state.
var errStaleDispatch = errors.New("stale dispatch lease")

func (c *Consumer) readerFactory() messageReaderFactory {
	if c.newMessageReader != nil {
		return c.newMessageReader
	}
	return func(queue, groupID string) messageReader {
		return newAmqpReader(c.amqpURL, queue, groupID, c.prefetch)
	}
}

func (c *Consumer) restartBackoff() time.Duration {
	if c.readerRestartBackoff > 0 {
		return c.readerRestartBackoff
	}
	return time.Second
}

// consumeMessages is the at-least-once consume loop. On handler success the
// delivery is Acked; on handler failure it is Nacked with requeue=true so
// RabbitMQ redelivers. Any consume/ack/nack infra error closes the reader;
// the outer runGroupConsumer rebuilds it after backoff.
func consumeMessages(ctx context.Context, reader messageReader, handler messageHandler) (err error) {
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("关闭 RabbitMQ reader 失败: %w", closeErr))
		}
	}()

	deliveries, err := reader.Consume(ctx)
	if err != nil {
		return fmt.Errorf("启动 RabbitMQ 消费失败: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("RabbitMQ 消费通道已关闭")
			}
			if handleErr := handler(ctx, delivery); handleErr != nil {
				// Handler failed: nack with requeue so RabbitMQ redelivers (at-least-once).
				// A poison message (unparseable / missing task) is isolated by
				// poisonAwareHandler which returns nil after persisting it, so it is
				// Acked here instead of redelivered forever.
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					return fmt.Errorf("处理消息失败且 nack 失败: handler=%w nack=%w", handleErr, nackErr)
				}
				continue
			}
			if ackErr := delivery.Ack(false); ackErr != nil {
				return fmt.Errorf("ack 消息失败: %w", ackErr)
			}
		}
	}
}

func (c *Consumer) runGroupConsumer(ctx context.Context, name string, queue, groupID string, handler messageHandler) {
	for ctx.Err() == nil {
		reader := c.readerFactory()(queue, groupID)
		observability.Log(ctx, slog.Default(), slog.LevelInfo, "rabbitmq consumer started", slog.String("consumer", name), slog.String("queue", queue), slog.String("group", groupID))
		err := consumeMessages(ctx, reader, handler)
		if ctx.Err() != nil {
			return
		}
		observability.Log(ctx, slog.Default(), slog.LevelWarn, "rabbitmq consumer rebuilding reader", slog.String("consumer", name), slog.String("error", observability.SafeError(err)))

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

// Group consumers Ack only after the handler either completes the business
// operation or durably records its failure for RetryScheduler; poison messages
// (unparseable / missing task) are persisted to kafka_message_failures then Acked
// so they do not block the queue. Any consume/ack/nack error closes this reader;
// the outer loop rebuilds it after backoff.
func (c *Consumer) poisonAwareHandler(name, groupID string, handler messageHandler) messageHandler {
	return func(ctx context.Context, delivery amqp.Delivery) error {
		err := handler(ctx, delivery)
		if err == nil || !isPoisonMessageError(err) {
			return err
		}
		if c == nil || c.repo == nil || c.repo.TaskMessageFailure == nil {
			return fmt.Errorf("poison 消息隔离仓储未初始化: %w", err)
		}
		failure := &model.KafkaMessageFailure{
			ConsumerGroup: groupID, ConsumerName: name, Topic: delivery.RoutingKey,
			Partition: 0, MessageOffset: int64(delivery.DeliveryTag),
			MessageKey: []byte(delivery.MessageId), Payload: append([]byte(nil), delivery.Body...),
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

func (c *Consumer) startGroupConsumer(ctx context.Context, name string, brokers []string, queue, groupID string, handler messageHandler) {
	durableHandler := c.poisonAwareHandler(name, groupID, c.dedupHandler(queue, handler))
	observedHandler := func(ctx context.Context, delivery amqp.Delivery) error {
		startedAt := time.Now()
		err := durableHandler(ctx, delivery)
		if metrics := observability.DefaultMetrics(); metrics != nil {
			metrics.ObserveKafkaJob(name, time.Since(startedAt))
		}
		return err
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.runGroupConsumer(ctx, name, queue, groupID, observedHandler)
	}()
}

// dedupHandler wraps a handler with the consumer-side message-level idempotency
// gate. A repeat delivery of the same MessageId is Acked as a no-op so the
// business logic runs exactly once per MessageId. On handler FAILURE the key
// is released before the outer Nack so RabbitMQ's requeue redelivery (same
// MessageId) is not suppressed — transient failures still get at-least-once
// redelivery. On business success the key is intentionally retained so a crash
// between Acquire and Ack still suppresses a duplicate. On checker error the
// delivery is returned as an error so the outer loop Nacks it for redelivery
// rather than risking a duplicate execution.
func (c *Consumer) dedupHandler(queue string, handler messageHandler) messageHandler {
	return func(ctx context.Context, delivery amqp.Delivery) error {
		if c.idempotency != nil && delivery.MessageId != "" {
			handled, err := c.idempotency.Acquire(ctx, queue, delivery.MessageId)
			if err != nil {
				return fmt.Errorf("%w: %v", errIdempotencyUnavailable, err)
			}
			if handled {
				return nil // duplicate delivery suppressed; Ack as no-op
			}
			err = handler(ctx, delivery)
			if err != nil {
				// Release the dedup key so the Nack-requeued redelivery (same
				// MessageId) is not suppressed — transient failures retry.
				_ = c.idempotency.Release(ctx, queue, delivery.MessageId)
				if errors.Is(err, errStaleDispatch) {
					// Stale dispatch lease: the message is outdated but the task is
					// not terminal. Ack it as a no-op instead of redelivering, so the
					// RetryScheduler's fresh dispatch (same MessageId) is not
					// suppressed by this stale message's dedup key.
					return nil
				}
			}
			return err
		}
		return handler(ctx, delivery)
	}
}

// Wait blocks until all started group consumers have stopped.
func (c *Consumer) Wait() {
	c.wg.Wait()
}

func (c *Consumer) StartAnalyzeConsumer(ctx context.Context, brokers []string, queue, groupID string) {
	c.startGroupConsumer(ctx, "analyze", brokers, queue, groupID, c.handleAnalyze)
}

func (c *Consumer) StartTranscribeConsumer(ctx context.Context, brokers []string, queue, groupID string) {
	c.startGroupConsumer(ctx, "transcribe", brokers, queue, groupID, c.handleTranscribe)
}

func (c *Consumer) StartDownloadConsumer(ctx context.Context, brokers []string, queue, groupID string) {
	c.startGroupConsumer(ctx, "download", brokers, queue, groupID, c.handleDownload)
}

func (c *Consumer) StartRAGIndexConsumer(ctx context.Context, brokers []string, queue, groupID string) {
	c.startGroupConsumer(ctx, "rag_index", brokers, queue, groupID, c.handleRAGIndex)
}
