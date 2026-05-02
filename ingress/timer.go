package ingress

import (
	"context"
	"errors"
	"fmt"
	"grctl/server/natsreg"
	"log/slog"
	"sync"
	"time"

	intr "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/vmihailenco/msgpack/v5"
)

type TimerHandler func(ctx context.Context, t ext.Timer, numDelivered uint64) intr.HandleResult

// timerStream manages timer scheduling and consumption using NATS JetStream.
type timerStream struct {
	js         jetstream.JetStream
	consumer   jetstream.Consumer
	consumeCtx jetstream.ConsumeContext
	stream     jetstream.Stream
	handler    TimerHandler
	wg         sync.WaitGroup
}

// NewTimerStream creates a TimerStream, ensuring the consumer exist.
func NewTimerStream(ctx context.Context, js jetstream.JetStream, stream jetstream.Stream) (*timerStream, error) {
	ts := &timerStream{js: js, stream: stream}

	consumer, err := ts.ensureTimerFiredConsumer(ctx)
	if err != nil {
		return nil, err
	}
	ts.consumer = consumer
	return ts, nil
}

// AddTimer schedules a timer to be delivered at the specified expiration time.
// The timer must have either a Directive or Command set.
func (ts *timerStream) AddTimer(ctx context.Context, timer ext.Timer, expiresAt time.Time) error {
	hasDirective := timer.Directive.ID != ""
	hasCommand := timer.Command.ID != ""
	if !hasDirective && !hasCommand {
		return fmt.Errorf("timer must have either Directive or Command set")
	}

	// Marshal timer to msgpack
	data, err := msgpack.Marshal(&timer)
	if err != nil {
		return fmt.Errorf("failed to marshal timer: %w", err)
	}

	// Construct subject for this timer
	subject := natsreg.Manifest.TimerSubject(timer.WFID, timer.Kind, timer.ID)

	// Publish with NATS scheduling headers
	msg := nats.NewMsg(subject)
	msg.Data = data
	msg.Header.Set("Nats-Schedule", fmt.Sprintf("@at %s", expiresAt.Format(time.RFC3339Nano)))
	msg.Header.Set("Nats-Schedule-Target", natsreg.Manifest.TimerFiredSubject())

	if _, err := ts.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish timer message: %w", err)
	}

	slog.Info("Timer scheduled",
		"kind", timer.Kind,
		"wfID", timer.WFID,
		"expiresAt", expiresAt,
	)

	return nil
}

// CancelTimer cancels a scheduled timer for the given workflow, kind, and timer ID.
// This operation is idempotent - it will succeed even if the timer has already fired or never existed.
func (ts *timerStream) CancelTimer(ctx context.Context, wfID ext.WFID, kind ext.TimerKind, timerID ext.TimerID) error {
	subject := natsreg.Manifest.TimerSubject(wfID, kind, timerID)

	err := ts.stream.Purge(ctx, jetstream.WithPurgeSubject(subject))
	if err != nil {
		// Ignore "no messages" errors - timer may have already fired
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			slog.Debug("Timer already fired or doesn't exist",
				"kind", kind,
				"wfID", wfID,
				"subject", subject)
			return nil
		}
		return fmt.Errorf("failed to purge timer subject: %w", err)
	}

	slog.Debug("Timer canceled",
		"kind", kind,
		"wfID", wfID,
		"subject", subject)

	return nil
}

func (ts *timerStream) GetTimer(ctx context.Context, wfID ext.WFID, kind ext.TimerKind, timerID ext.TimerID) (ext.Timer, error) {
	subject := natsreg.Manifest.TimerSubject(wfID, kind, timerID)
	msg, err := ts.stream.GetLastMsgForSubject(ctx, subject)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return ext.Timer{}, nil
		}
		return ext.Timer{}, fmt.Errorf("failed to get timer message: %w", err)
	}

	var timer ext.Timer
	if err := msgpack.Unmarshal(msg.Data, &timer); err != nil {
		return ext.Timer{}, fmt.Errorf("failed to unmarshal timer message: %w", err)
	}

	return timer, nil
}

// Start begins consuming fired timers using the provided handler.
// The handler returns a HandleResult which is translated to NATS Ack/Nak operations.
func (ts *timerStream) Start(handler TimerHandler) error {
	if ts.consumer == nil {
		return fmt.Errorf("timer consumer is not initialized")
	}
	if handler == nil {
		return fmt.Errorf("timer handler is not provided")
	}

	ts.handler = handler

	cons, err := ts.consumer.Consume(
		ts.processMessage,
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			if errors.Is(err, jetstream.ErrServerShutdown) {
				return
			}
			slog.Error("Timer consumer error", "error", err)
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to start timer consumer: %w", err)
	}

	ts.consumeCtx = cons
	slog.Debug("Timer consumer started")
	return nil
}

// Stop stops timer consumption.
func (ts *timerStream) Stop() {
	if ts.consumeCtx != nil {
		ts.consumeCtx.Stop()
	}
	ts.wg.Wait()
	slog.Debug("Timer consumer stopped")
}

// processMessage processes a fired timer message and translates HandleResult to NATS operations
func (ts *timerStream) processMessage(msg jetstream.Msg) {
	ts.wg.Add(1)
	defer ts.wg.Done()

	var timer ext.Timer
	if err := msgpack.Unmarshal(msg.Data(), &timer); err != nil {
		slog.Error("Failed to unmarshal timer, acknowledging to prevent retry",
			"error", err)
		if ackErr := msg.Ack(); ackErr != nil {
			slog.Error("Failed to Ack malformed timer message", "error", ackErr)
		}
		return
	}

	numDelivered := uint64(1)
	if md, err := msg.Metadata(); err != nil {
		slog.Warn("Failed to read timer message metadata, defaulting delivery count", "error", err)
	} else {
		numDelivered = uint64(md.NumDelivered)
	}

	ctx, cancel := context.WithTimeout(context.Background(), processingTimeout)
	defer cancel()

	result := ts.handler(ctx, timer, numDelivered)

	// Translate result to NATS operations
	switch result.Action {
	case intr.ActionProcessed:
		if err := msg.Ack(); err != nil {
			slog.Error("Failed to Ack timer message", "error", err)
		}

	case intr.ActionRetryable:
		if err := msg.NakWithDelay(result.RetryDelay); err != nil {
			slog.Error("Failed to Nak timer message with delay", "error", err)
		}

	default:
		slog.Error("Unknown handler action, defaulting to Nak", "action", result.Action)
		if err := msg.Nak(); err != nil {
			slog.Error("Failed to Nak timer message", "error", err)
		}
	}
}

// ensureTimerFiredConsumer creates or updates the timer fired consumer
func (ts *timerStream) ensureTimerFiredConsumer(ctx context.Context) (jetstream.Consumer, error) {
	cfg := jetstream.ConsumerConfig{
		Name:          natsreg.Manifest.TimerFiredConsumerName(),
		Durable:       natsreg.Manifest.TimerFiredConsumerName(),
		FilterSubject: natsreg.Manifest.TimerFiredSubject(),
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	}

	consumer, err := ts.stream.CreateOrUpdateConsumer(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create timer fired consumer: %w", err)
	}

	return consumer, nil
}
