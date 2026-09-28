package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	amqp "github.com/rabbitmq/amqp091-go"
	"log"
	"strings"
	"sync"
	"time"
	"vid-lens/internal/artifact"
	"vid-lens/internal/model"
	"vid-lens/internal/repository"
)

const (
	ArtifactQueue     = "vidlens.artifact.generate.v1"
	ArtifactEditQueue = "vidlens.artifact.edit.v1"

	artifactGenerationSubject = "generation_request"
)

var errArtifactQueueMismatch = errors.New("artifact run delivered to wrong queue")

type ArtifactDispatch struct {
	SchemaVersion int    `json:"schema_version"`
	RunID         string `json:"run_id"`
	DispatchID    string `json:"dispatch_id"`
	TraceID       string `json:"trace_id"`
}

// ArtifactWorker has its own queue, context and bounded consumer. It does not mutate VideoTask.
type ArtifactWorker struct {
	repo *repository.ArtifactRepository
	svc  ArtifactExecutor
	url  string
	wg   sync.WaitGroup
}

type ArtifactExecutor interface {
	ExecuteArtifact(context.Context, string) error
	ExecuteArtifactEdit(context.Context, string) error
}

func NewArtifactWorker(repo *repository.ArtifactRepository, svc ArtifactExecutor, brokers []string) *ArtifactWorker {
	url := ""
	if len(brokers) > 0 {
		url = brokers[0]
		if !strings.Contains(url, "://") {
			url = "amqp://" + url
		}
	}
	return &ArtifactWorker{repo: repo, svc: svc, url: url}
}
func (w *ArtifactWorker) Start(ctx context.Context) {
	w.wg.Add(4)
	go func() { defer w.wg.Done(); w.dispatch(ctx) }()
	go func() { defer w.wg.Done(); w.dispatchEdits(ctx) }()
	go func() { defer w.wg.Done(); w.consume(ctx, ArtifactQueue) }()
	go func() { defer w.wg.Done(); w.consume(ctx, ArtifactEditQueue) }()
}
func (w *ArtifactWorker) Wait() { w.wg.Wait() }
func (w *ArtifactWorker) dispatch(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	nextCleanup := time.Now()
	for ctx.Err() == nil {
		if time.Now().After(nextCleanup) {
			if err := w.repo.Prune(ctx); err != nil && ctx.Err() == nil {
				log.Print("artifact retention cleanup temporarily unavailable")
			}
			nextCleanup = time.Now().Add(time.Hour)
		}
		if err := w.repo.Recover(ctx); err != nil && ctx.Err() == nil {
			log.Print("artifact recovery temporarily unavailable")
		}
		rows, err := w.repo.Dispatches(ctx)
		if err == nil {
			for _, d := range rows {
				publishCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err = w.publish(publishCtx, ArtifactQueue, d.SubjectKind, ArtifactDispatch{1, d.RunID, d.ID, d.RunID})
				cancel()
				if e := w.repo.DispatchResult(ctx, d.GenerationDispatch, err == nil); e != nil && ctx.Err() == nil {
					log.Print("artifact outbox result temporarily unavailable")
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *ArtifactWorker) dispatchEdits(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		rows, err := w.repo.EditDispatches(ctx)
		if err == nil {
			for _, d := range rows {
				publishCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err = w.publish(publishCtx, ArtifactEditQueue, d.SubjectKind, ArtifactDispatch{1, d.RunID, d.ID, d.RunID})
				cancel()
				if e := w.repo.EditDispatchResult(ctx, d.ArtifactEditDispatch, err == nil); e != nil && ctx.Err() == nil {
					log.Print("artifact edit outbox result temporarily unavailable")
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func artifactSubjectForQueue(queue string) (string, error) {
	switch queue {
	case ArtifactQueue:
		return artifactGenerationSubject, nil
	case ArtifactEditQueue:
		return model.AgentRunSubjectArtifactEdit, nil
	default:
		return "", fmt.Errorf("unsupported artifact queue %q", queue)
	}
}

func (w *ArtifactWorker) publish(ctx context.Context, queue, dispatchedSubject string, payload ArtifactDispatch) error {
	expectedSubject, err := artifactSubjectForQueue(queue)
	if err != nil {
		return err
	}
	if dispatchedSubject != expectedSubject {
		return fmt.Errorf("%w: expected=%s dispatched=%s run_id=%s", errArtifactQueueMismatch, expectedSubject, dispatchedSubject, payload.RunID)
	}
	actualSubject, err := w.repo.ArtifactRunSubject(ctx, payload.RunID)
	if err != nil {
		return err
	}
	if actualSubject != dispatchedSubject {
		return fmt.Errorf("%w: expected=%s actual=%s run_id=%s", errArtifactQueueMismatch, dispatchedSubject, actualSubject, payload.RunID)
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
	if _, err = ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		return err
	}
	if err = ch.Confirm(false); err != nil {
		return err
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	returns := ch.NotifyReturn(make(chan amqp.Return, 1))
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err = ch.PublishWithContext(ctx, "", queue, true, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: payload.RunID + ":" + payload.DispatchID, Body: raw, Timestamp: time.Now().UTC()}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-returns:
		return errors.New("artifact dispatch unroutable")
	case confirmed, ok := <-confirms:
		if !ok || !confirmed.Ack {
			return errors.New("artifact dispatch unconfirmed")
		}
		select {
		case <-returns:
			return errors.New("artifact dispatch unroutable")
		default:
			return nil
		}
	}
}

func (w *ArtifactWorker) consume(ctx context.Context, queue string) {
	if _, err := artifactSubjectForQueue(queue); err != nil {
		return
	}
	consumer := "vidlens-artifact-worker"
	if queue == ArtifactEditQueue {
		consumer = "vidlens-artifact-edit-worker"
	}
	for ctx.Err() == nil {
		reader := newAmqpReader(w.url, queue, consumer, 1)
		if reader.ch != nil {
			_, _ = reader.ch.QueueDeclare(queue, true, false, false, false, nil)
		}
		_ = consumeMessages(ctx, reader, func(ctx context.Context, d amqp.Delivery) error {
			return w.handle(ctx, queue, d)
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (w *ArtifactWorker) handle(ctx context.Context, queue string, d amqp.Delivery) error {
	expectedSubject, err := artifactSubjectForQueue(queue)
	if err != nil {
		return err
	}
	var p ArtifactDispatch
	if len(d.Body) > 4096 || json.Unmarshal(d.Body, &p) != nil || p.SchemaVersion != 1 || p.RunID == "" || p.DispatchID == "" {
		return nil
	}
	subjectKind, err := w.repo.ArtifactRunSubject(ctx, p.RunID)
	if err != nil {
		var domain *artifact.Error
		if errors.As(err, &domain) && domain.Code == "not_found" {
			return nil
		}
		return err
	}
	if subjectKind != expectedSubject {
		return nil
	}
	if subjectKind == model.AgentRunSubjectArtifactEdit {
		err = w.svc.ExecuteArtifactEdit(ctx, p.RunID)
	} else {
		err = w.svc.ExecuteArtifact(ctx, p.RunID)
	}
	var domain *artifact.Error
	if errors.As(err, &domain) && domain.Code == "not_found" {
		return nil
	}
	return err
}
