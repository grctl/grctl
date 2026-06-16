package run

import (
	"context"
	"grctl/server/run/record"
	model "grctl/server/types"
	ext "grctl/server/types/external/v1"
	"log/slog"
	"time"
)

type ctxKey string

const (
	ctxKeyWFID         ctxKey = "wf_id"
	ctxKeyRunID        ctxKey = "run_id"
	ctxKeyDirective    ctxKey = "directive"
	ctxKeyNumDelivered ctxKey = "num_delivered"
	ctxKeyRunStateKind ctxKey = "runStateKind"
)

// RetryDelay is the delay between retry attempts when a directive cannot be processed because of storage failures.
var RetryDelay = 100 * time.Millisecond

type Recorder interface {
	GetStateSnapshot(ctx context.Context, wfID ext.WFID, runID ext.RunID) (model.StateSnapshot, error)
	Commit(ctx context.Context, records []model.Record) (model.CommitResult, error)
}

type Manager struct {
	recorder             Recorder
	defaultStepTimeoutMS uint32
	defaultWaitTimeoutMS uint32
}

func NewManager(recorder Recorder, defaultStepTimeoutMS uint32, defaultWaitTimeoutMS uint32) *Manager {
	return &Manager{
		recorder:             recorder,
		defaultStepTimeoutMS: defaultStepTimeoutMS,
		defaultWaitTimeoutMS: defaultWaitTimeoutMS,
	}
}

// Handle processes a directive for a run, applying the appropriate state updates.
// Receives the directives from the directive consumer.
// As a result, commits the state updates and returns Handle Result. So, the caller can decide retry or not.
func (m *Manager) Handle(ctx context.Context, d ext.Directive, numDelivered uint64) model.HandleResult {
	slog.DebugContext(ctx, "handling directive", "kind", d.Kind)
	ctx = context.WithValue(ctx, ctxKeyWFID, d.RunInfo.WFID)
	ctx = context.WithValue(ctx, ctxKeyRunID, d.RunInfo.ID)
	ctx = context.WithValue(ctx, ctxKeyDirective, d.Kind)
	ctx = context.WithValue(ctx, ctxKeyNumDelivered, numDelivered)

	// Get RunState, pending Cancel and pending Event
	sn, err := m.recorder.GetStateSnapshot(ctx, d.RunInfo.WFID, d.RunInfo.ID)
	if err != nil {
		slog.Error("failed to get state snapshot", "error", err)
		return model.Retryable(RetryDelay)
	}

	ctx = context.WithValue(ctx, ctxKeyRunStateKind, sn.RunState.Kind)

	// plan owns the terminal-run guard: a directive for a finished run yields no
	// records, which is a no-op (Processed).
	records, err := plan(ctx, d, sn, m.defaultStepTimeoutMS, m.defaultWaitTimeoutMS)
	if err != nil {
		slog.Error("failed to create plan", "error", err)
		return model.Retryable(RetryDelay)
	}

	if len(records) == 0 {
		return model.Processed()
	}

	result, err := m.commit(ctx, records)
	if err != nil {
		slog.Error("failed to apply state updates", "error", err)
		return m.failAndCommit(ctx, d, sn.RunState, err)
	}

	return result
}

func (m *Manager) commit(ctx context.Context, updates []model.Record) (model.HandleResult, error) {
	result, err := m.recorder.Commit(ctx, updates)
	if err != nil {
		if result.IsCASRejection {
			// A CAS rejection, a concurrent update to the run state,
			// which could be due to another directive being processed at the same time.
			// Retrying after a delay allows the system to resolve the conflict and apply the updates successfully.
			return model.Retryable(RetryDelay), nil
		}

		if result.IsDuplicateMessage {
			// Idempotent handling: if it's a duplicate message, we can consider it processed successfully.
			return model.Processed(), nil
		}

		if result.IsAtomicPublishBackpressure {
			// The server is at its concurrent atomic-batch limit. This is transient backpressure,
			// not a permanent failure — retry after a short delay.
			return model.Retryable(RetryDelay), nil
		}

		return model.HandleResult{}, err
	}

	return model.Processed(), nil
}

func (m *Manager) failAndCommit(ctx context.Context, d ext.Directive, currentState ext.RunState, cause error) model.HandleResult {
	failureUpdates, err := record.BuildUnexpectedFailure(d, cause.Error(), currentState)
	if err != nil {
		slog.Error("failed to build failure updates", "error", err)
		return model.Processed()
	}

	// If CAS rejection happens at that point, failAndCommit will retry the failure updates.
	// But concrete failure will be logged and the result will be processed.
	result, err := m.commit(ctx, failureUpdates)
	if err != nil {
		slog.Error("failed to apply failure updates", "error", err)
		return model.Processed()
	}
	return result
}
