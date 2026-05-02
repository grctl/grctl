package store

import (
	"context"
	"fmt"
	"grctl/server/natsreg"

	"github.com/nats-io/nats.go/jetstream"
)

func EnsureStateStream(ctx context.Context, js jetstream.JetStream, inMemory bool) (jetstream.Stream, error) {
	storage := jetstream.FileStorage
	if inMemory {
		storage = jetstream.MemoryStorage
	}

	cfg := jetstream.StreamConfig{
		Name:      natsreg.Manifest.StateStreamName(),
		Retention: jetstream.LimitsPolicy,
		Subjects: []string{
			natsreg.Manifest.DirectiveListenerPattern(),
			natsreg.Manifest.HistoryListenerPattern(),
			natsreg.Manifest.TimerListenerPattern(),
			natsreg.Manifest.TimerFiredSubject(),
			natsreg.Manifest.AllRunsInfoKeyPattern(),
			natsreg.Manifest.AllRunsInputKeyPattern(),
			natsreg.Manifest.AllRunsOutputKeyPattern(),
			natsreg.Manifest.AllRunsErrorKeyPattern(),
			natsreg.Manifest.AllWfKVKeyPattern(),
			natsreg.Manifest.EventInboxListenerPattern(),
			natsreg.Manifest.CancelListenerPattern(),
			natsreg.Manifest.RunStateListenerPattern(),
			natsreg.Manifest.WorkerTaskListenerPattern(),
			natsreg.Manifest.BgTaskSubject(),
		},
		Storage:            storage,
		AllowDirect:        true,
		AllowMsgSchedules:  true,
		AllowAtomicPublish: true,
		AllowRollup:        true,
	}

	stream, err := js.CreateOrUpdateStream(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create state stream: %w", err)
	}

	return stream, nil
}
