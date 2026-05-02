package ingress

import (
	"context"
	"errors"
	"fmt"
	"grctl/server/natsreg"
	"log/slog"
	"sync"

	intr "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/vmihailenco/msgpack/v5"
)

type BgTaskHandler func(ctx context.Context, task ext.BackgroundTask, numDelivered uint64) intr.HandleResult

type bgTaskQueue struct {
	stream     jetstream.Stream
	consumer   jetstream.Consumer
	consumeCtx jetstream.ConsumeContext
	handler    BgTaskHandler
	wg         sync.WaitGroup
}

func NewBgTaskQueue(ctx context.Context, stream jetstream.Stream) (*bgTaskQueue, error) {
	q := &bgTaskQueue{stream: stream}

	consumer, err := q.ensureConsumer(ctx)
	if err != nil {
		return nil, err
	}
	q.consumer = consumer
	return q, nil
}

func (q *bgTaskQueue) Start(handler BgTaskHandler) error {
	if q.consumer == nil {
		return fmt.Errorf("background task consumer is not initialized")
	}
	if handler == nil {
		return fmt.Errorf("background task handler is not provided")
	}

	q.handler = handler

	cons, err := q.consumer.Consume(
		q.processMessage,
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			if errors.Is(err, jetstream.ErrServerShutdown) {
				return
			}
			slog.Error("Background task consumer error", "error", err)
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to start background task consumer: %w", err)
	}

	q.consumeCtx = cons
	slog.Debug("Background task consumer started")
	return nil
}

func (q *bgTaskQueue) Stop() {
	if q.consumeCtx != nil {
		q.consumeCtx.Stop()
	}
	q.wg.Wait()
	slog.Debug("Background task consumer stopped")
}

func (q *bgTaskQueue) processMessage(msg jetstream.Msg) {
	q.wg.Add(1)
	defer q.wg.Done()

	var task ext.BackgroundTask
	if err := msgpack.Unmarshal(msg.Data(), &task); err != nil {
		slog.Error("Failed to unmarshal background task, acknowledging to prevent retry", "error", err)
		if ackErr := msg.Ack(); ackErr != nil {
			slog.Error("Failed to Ack malformed background task message", "error", ackErr)
		}
		return
	}

	numDelivered := uint64(1)
	if md, err := msg.Metadata(); err != nil {
		slog.Warn("Failed to read background task message metadata, defaulting delivery count", "error", err)
	} else {
		numDelivered = uint64(md.NumDelivered)
	}

	ctx, cancel := context.WithTimeout(context.Background(), processingTimeout)
	defer cancel()

	result := q.handler(ctx, task, numDelivered)

	switch result.Action {
	case intr.ActionProcessed:
		if err := msg.Ack(); err != nil {
			slog.Error("Failed to Ack background task message", "error", err)
		}

	case intr.ActionRetryable:
		if err := msg.NakWithDelay(result.RetryDelay); err != nil {
			slog.Error("Failed to Nak background task message with delay", "error", err)
		}

	default:
		slog.Error("Unknown handler action for background task, defaulting to Nak", "action", result.Action)
		if err := msg.Nak(); err != nil {
			slog.Error("Failed to Nak background task message", "error", err)
		}
	}
}

func (q *bgTaskQueue) ensureConsumer(ctx context.Context) (jetstream.Consumer, error) {
	cfg := jetstream.ConsumerConfig{
		Name:          natsreg.Manifest.BgTaskConsumerName(),
		Durable:       natsreg.Manifest.BgTaskConsumerName(),
		FilterSubject: natsreg.Manifest.BgTaskSubject(),
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
	}

	consumer, err := q.stream.CreateOrUpdateConsumer(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create background task consumer: %w", err)
	}

	return consumer, nil
}
