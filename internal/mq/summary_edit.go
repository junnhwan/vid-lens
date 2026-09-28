package mq

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

const SummaryEditQueue = "vidlens.summary.edit.v1"

type SummaryEditExecutor interface {
	ExecuteSummaryEdit(context.Context, string) error
}

type SummaryEditWorker struct {
	repo *repository.SummaryRevisionRepository
	svc  SummaryEditExecutor
	url  string
	wg   sync.WaitGroup
}

func NewSummaryEditWorker(repo *repository.SummaryRevisionRepository, svc SummaryEditExecutor, brokers []string) *SummaryEditWorker {
	url := ""
	if len(brokers) > 0 {
		url = brokers[0]
		if !strings.Contains(url, "://") {
			url = "amqp://" + url
		}
	}
	return &SummaryEditWorker{repo: repo, svc: svc, url: url}
}

func (w *SummaryEditWorker) Start(ctx context.Context) {
	w.wg.Add(2)
	go func() { defer w.wg.Done(); w.dispatch(ctx) }()
	go func() { defer w.wg.Done(); w.consume(ctx) }()
}
func (w *SummaryEditWorker) Wait() { w.wg.Wait() }

func (w *SummaryEditWorker) dispatch(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := w.repo.Recover(ctx); err != nil && ctx.Err() == nil {
			log.Print("summary edit recovery temporarily unavailable")
		}
		rows, err := w.repo.Dispatches(ctx)
		if err != nil && ctx.Err() == nil {
			log.Print("summary edit outbox temporarily unavailable")
		}
		for _, row := range rows {
			publishCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := w.publish(publishCtx, row)
			cancel()
			if e := w.repo.DispatchResult(ctx, row.SummaryEditDispatch, err == nil); e != nil && ctx.Err() == nil {
				log.Print("summary edit outbox result temporarily unavailable")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *SummaryEditWorker) publish(ctx context.Context, row repository.SummaryDispatchIntent) error {
	if row.SubjectKind != model.AgentRunSubjectSummaryEdit {
		return errArtifactQueueMismatch
	}
	subject, err := w.repo.RunSubject(ctx, row.RunID)
	if err != nil {
		return err
	}
	if subject != model.AgentRunSubjectSummaryEdit {
		return errArtifactQueueMismatch
	}
	conn, err := amqp.DialConfig(w.url, amqp.Config{Dial: amqp.DefaultDial(5 * time.Second)})
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if _, err = ch.QueueDeclare(SummaryEditQueue, true, false, false, false, nil); err != nil {
		return err
	}
	if err = ch.Confirm(false); err != nil {
		return err
	}
	confirms, returns := ch.NotifyPublish(make(chan amqp.Confirmation, 1)), ch.NotifyReturn(make(chan amqp.Return, 1))
	raw, err := json.Marshal(ArtifactDispatch{SchemaVersion: 1, RunID: row.RunID, DispatchID: row.ID, TraceID: row.RunID})
	if err != nil {
		return err
	}
	if err = ch.PublishWithContext(ctx, "", SummaryEditQueue, true, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: row.RunID + ":" + row.ID, Body: raw, Timestamp: time.Now().UTC()}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-returns:
		return errors.New("summary edit dispatch unroutable")
	case confirm, ok := <-confirms:
		if !ok || !confirm.Ack {
			return errors.New("summary edit dispatch unconfirmed")
		}
		select {
		case <-returns:
			return errors.New("summary edit dispatch unroutable")
		default:
			return nil
		}
	}
}

func (w *SummaryEditWorker) consume(ctx context.Context) {
	for ctx.Err() == nil {
		reader := newAmqpReader(w.url, SummaryEditQueue, "vidlens-summary-edit-worker", 1)
		if reader.ch != nil {
			_, _ = reader.ch.QueueDeclare(SummaryEditQueue, true, false, false, false, nil)
		}
		_ = consumeMessages(ctx, reader, func(ctx context.Context, d amqp.Delivery) error { return w.handle(ctx, d) })
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (w *SummaryEditWorker) handle(ctx context.Context, d amqp.Delivery) error {
	var payload ArtifactDispatch
	if len(d.Body) > 4096 || json.Unmarshal(d.Body, &payload) != nil || payload.SchemaVersion != 1 || payload.RunID == "" || payload.DispatchID == "" {
		return nil
	}
	subject, err := w.repo.RunSubject(ctx, payload.RunID)
	if err != nil {
		var domain *artifact.Error
		if errors.As(err, &domain) && domain.Code == "not_found" {
			return nil
		}
		return err
	}
	if subject != model.AgentRunSubjectSummaryEdit {
		return nil
	}
	err = w.svc.ExecuteSummaryEdit(ctx, payload.RunID)
	var domain *artifact.Error
	if errors.As(err, &domain) && domain.Code == "not_found" {
		return nil
	}
	return err
}
