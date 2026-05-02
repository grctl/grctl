package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/synadia-io/orbit.go/jetstreamext"
)

type UpdateService struct {
	js jetstream.JetStream
}

func NewUpdateService(js jetstream.JetStream) UpdateService {
	return UpdateService{js: js}
}

func (s *UpdateService) SaveUpdates(ctx context.Context, updates []StateUpdate) error {
	if len(updates) < 1 {
		return fmt.Errorf("transition requires at least 1 updates, got %d", len(updates))
	}

	var msgs []*nats.Msg
	for _, update := range updates {
		updateMsgs, err := update.ToNatsMsgs()
		if err != nil {
			return fmt.Errorf("failed to create NATS messages: %w", err)
		}
		msgs = append(msgs, updateMsgs...)
	}

	if len(msgs) == 0 {
		return fmt.Errorf("no messages to publish")
	}

	return s.BatchPublish(ctx, msgs)
}

func (s *UpdateService) BatchPublish(ctx context.Context, msgs []*nats.Msg) error {
	if len(msgs) < 1 {
		return fmt.Errorf("transition requires at least 1 updates, got %d", len(msgs))
	}

	batch, err := jetstreamext.NewBatchPublisher(s.js)
	if err != nil {
		return fmt.Errorf("failed to create batch publisher: %w", err)
	}

	for i := 0; i < len(msgs)-1; i++ {
		err = batch.AddMsg(msgs[i])
		if err != nil {
			return fmt.Errorf("failed to add message to batch: %w", err)
		}
	}

	_, err = batch.CommitMsg(ctx, msgs[len(msgs)-1])
	if err != nil {
		return fmt.Errorf("failed to commit batch (last_subject=%s, subjects=%s): %w", msgs[len(msgs)-1].Subject, collectSubjects(msgs), err)
	}

	return nil
}

func collectSubjects(msgs []*nats.Msg) string {
	subjects := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		subjects = append(subjects, msg.Subject)
	}
	return strings.Join(subjects, ",")
}
