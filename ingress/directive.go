package ingress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"grctl/server/natsreg"

	intr "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/vmihailenco/msgpack/v5"
)

const processingTimeout = 25 * time.Second

type DirectiveHandler func(ctx context.Context, d ext.Directive, numDelivered uint64) intr.HandleResult

type directiveQueue struct {
	js         jetstream.JetStream
	stream     jetstream.Stream
	consumer   jetstream.Consumer
	consumeCtx jetstream.ConsumeContext
	handler    DirectiveHandler
	wg         sync.WaitGroup
}

func NewDirectiveQueue(ctx context.Context, js jetstream.JetStream, stream jetstream.Stream) (*directiveQueue, error) {
	q := &directiveQueue{js: js, stream: stream}

	consumer, err := q.ensureConsumer(ctx)
	if err != nil {
		return nil, err
	}
	q.consumer = consumer
	return q, nil
}

func (q *directiveQueue) Start(handler DirectiveHandler) error {
	if q.consumer == nil {
		return fmt.Errorf("directive consumer is not initialized")
	}
	if handler == nil {
		return fmt.Errorf("directive handler is not provided")
	}

	q.handler = handler

	cons, err := q.consumer.Consume(
		q.processMessage,
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			if errors.Is(err, jetstream.ErrServerShutdown) {
				return
			}
			slog.Error("Directive consumer error", "error", err)
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to start directive consumer: %w", err)
	}

	q.consumeCtx = cons
	slog.Debug("Directive consumer started")
	return nil
}

func (q *directiveQueue) Stop() {
	if q.consumeCtx != nil {
		q.consumeCtx.Stop()
	}
	q.wg.Wait()
	slog.Debug("Directive consumer stopped")
}

// processMessage deserializes the NATS message into a Directive,
// passes it to the handler, and translates HandleResult to NATS Ack/Nak operations.
func (q *directiveQueue) processMessage(msg jetstream.Msg) {
	q.wg.Add(1)
	defer q.wg.Done()

	var directive ext.Directive
	if err := msgpack.Unmarshal(msg.Data(), &directive); err != nil {
		slog.Error("Failed to unmarshal directive, acknowledging to prevent retry", "error", err)
		if ackErr := msg.Ack(); ackErr != nil {
			slog.Error("Failed to Ack malformed message", "error", ackErr)
		}
		return
	}

	numDelivered := uint64(1)
	if md, err := msg.Metadata(); err != nil {
		slog.Warn("Failed to read directive message metadata, defaulting delivery count", "error", err)
	} else {
		numDelivered = uint64(md.NumDelivered)
	}

	ctx, cancel := context.WithTimeout(context.Background(), processingTimeout)
	defer cancel()

	result := q.handler(ctx, directive, numDelivered)

	switch result.Action {
	case intr.ActionProcessed:
		if err := msg.Ack(); err != nil {
			slog.Error("Failed to Ack message", "error", err)
		}

	case intr.ActionRetryable:
		if err := msg.NakWithDelay(result.RetryDelay); err != nil {
			slog.Error("Failed to Nak message with delay", "error", err)
		}

	default:
		slog.Error("Unknown handler action, defaulting to Nak", "action", result.Action)
		if err := msg.Nak(); err != nil {
			slog.Error("Failed to Nak message", "error", err)
		}
	}
}

func (q *directiveQueue) ensureConsumer(ctx context.Context) (jetstream.Consumer, error) {
	cfg := jetstream.ConsumerConfig{
		Name:          natsreg.Manifest.DirectiveConsumerName(),
		Durable:       natsreg.Manifest.DirectiveConsumerName(),
		FilterSubject: natsreg.Manifest.DirectiveListenerPattern(),
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	}

	consumer, err := q.stream.CreateOrUpdateConsumer(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create directive consumer: %w", err)
	}

	return consumer, nil
}
