package jsstore

import (
	"context"
	"errors"
	"fmt"
	"grctl/server/natsreg"
	ext "grctl/server/types/external/v1"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/synadia-io/orbit.go/jetstreamext"
	"github.com/vmihailenco/msgpack/v5"
)

// ErrWorkflowTypeNotRegistered is returned when no registry entry exists for a
// workflow type.
var ErrWorkflowTypeNotRegistered = errors.New("workflow type not registered")

// RegistryEntry is the durable record of a single workflow type: its structural
// definition plus provenance for debugging. Exactly one entry is retained per
// type via per-subject rollup.
type RegistryEntry struct {
	ext.WorkflowTypeDef
	WorkerID     string    `msgpack:"worker_id"`
	RegisteredAt time.Time `msgpack:"registered_at"`
}

// WorkflowTypeRegistry is the server's durable store of workflow type
// definitions. It lives on the single state stream, one rolled-up subject per
// type, and is the source of truth for what types exist.
type WorkflowTypeRegistry struct {
	js     jetstream.JetStream
	stream jetstream.Stream
}

func NewWorkflowTypeRegistry(js jetstream.JetStream, stream jetstream.Stream) *WorkflowTypeRegistry {
	return &WorkflowTypeRegistry{js: js, stream: stream}
}

// PutTypes writes a worker's whole catalog atomically: either every type
// commits or none do. Each type is published with a per-subject rollup so its
// subject retains exactly the latest definition, keeping storage bounded
// regardless of worker count or restart churn. An empty catalog is a no-op.
func (r *WorkflowTypeRegistry) PutTypes(ctx context.Context, workerID string, defs []ext.WorkflowTypeDef) error {
	if len(defs) == 0 {
		return nil
	}

	registeredAt := time.Now().UTC()
	msgs := make([]*nats.Msg, 0, len(defs))
	for _, def := range defs {
		entry := RegistryEntry{WorkflowTypeDef: def, WorkerID: workerID, RegisteredAt: registeredAt}
		data, err := msgpack.Marshal(&entry)
		if err != nil {
			return fmt.Errorf("failed to marshal registry entry for type %s: %w", def.Type, err)
		}

		msg := nats.NewMsg(natsreg.Manifest.WorkflowTypeRegistrySubject(def.Type))
		msg.Data = data
		msg.Header.Set(jetstream.MsgRollup, jetstream.MsgRollupSubject)
		msgs = append(msgs, msg)
	}

	batch, err := jetstreamext.NewBatchPublisher(r.js)
	if err != nil {
		return fmt.Errorf("failed to create batch publisher: %w", err)
	}

	for i := 0; i < len(msgs)-1; i++ {
		if err := batch.AddMsg(msgs[i]); err != nil {
			return fmt.Errorf("failed to add registry entry to batch: %w", err)
		}
	}

	if _, err := batch.CommitMsg(ctx, msgs[len(msgs)-1]); err != nil {
		return fmt.Errorf("failed to commit registry batch: %w", err)
	}

	return nil
}

// GetType returns the stored definition for one workflow type via direct-get.
// Returns ErrWorkflowTypeNotRegistered if the type has never been registered.
func (r *WorkflowTypeRegistry) GetType(ctx context.Context, wfType ext.WFType) (RegistryEntry, error) {
	subject := natsreg.Manifest.WorkflowTypeRegistrySubject(wfType)
	msg, err := r.stream.GetLastMsgForSubject(ctx, subject)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return RegistryEntry{}, ErrWorkflowTypeNotRegistered
		}
		return RegistryEntry{}, fmt.Errorf("failed to get registry entry for type %s: %w", wfType, err)
	}

	var entry RegistryEntry
	if err := msgpack.Unmarshal(msg.Data, &entry); err != nil {
		return RegistryEntry{}, fmt.Errorf("failed to unmarshal registry entry for type %s: %w", wfType, err)
	}

	return entry, nil
}

// GetStartStepTimeout returns the start step timeout in milliseconds for the
// given workflow type. Returns ErrWorkflowTypeNotRegistered if the type has
// never been registered.
func (r *WorkflowTypeRegistry) GetStartStepTimeout(ctx context.Context, wfType ext.WFType) (uint32, error) {
	entry, err := r.GetType(ctx, wfType)
	if err != nil {
		return 0, err
	}
	return entry.StartStepTimeoutMS, nil
}

// ListTypes returns every registered type. It scans all registry subjects and
// is intended for operators and debugging, not any hot path.
func (r *WorkflowTypeRegistry) ListTypes(ctx context.Context) ([]RegistryEntry, error) {
	subject := natsreg.Manifest.WorkflowTypeRegistryListenerPattern()
	streamName := natsreg.Manifest.StateStreamName()
	msgs, err := jetstreamext.GetLastMsgsFor(ctx, r.js, streamName, []string{subject})
	if err != nil {
		return nil, fmt.Errorf("failed to scan registry entries: %w", err)
	}

	var entries []RegistryEntry
	for msg, err := range msgs {
		if errors.Is(err, jetstreamext.ErrNoMessages) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read registry entry: %w", err)
		}

		var entry RegistryEntry
		if err := msgpack.Unmarshal(msg.Data, &entry); err != nil {
			return nil, fmt.Errorf("failed to unmarshal registry entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}
