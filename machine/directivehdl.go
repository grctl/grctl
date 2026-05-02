package machine

import (
	"context"
	"errors"
	"grctl/server/store"
	intr "grctl/server/types"
	ext "grctl/server/types/external/v1"
	"log/slog"
	"time"
)

var NackDelay = 100 * time.Millisecond

type Storer interface {
	GetStateSnapshot(ctx context.Context, wfID ext.WFID, runID ext.RunID) (store.StateSnapshot, error)
	ApplyStateUpdates(ctx context.Context, updates []store.StateUpdate) error
}

type DirectiveHandler struct {
	store Storer
}

func NewDirectiveHandler(storer Storer) *DirectiveHandler {
	return &DirectiveHandler{
		store: storer,
	}
}

func (dh *DirectiveHandler) Handle(ctx context.Context, d ext.Directive, numDelivered uint64) intr.HandleResult {
	sn, err := dh.store.GetStateSnapshot(ctx, d.RunInfo.WFID, d.RunInfo.ID)
	if err != nil {
		return intr.Retryable(NackDelay)
	}
	if sn.RunState.IsTerminal() {
		slog.Debug("rejecting directive for terminal run", "directiveID", d.ID, "wfID", d.RunInfo.WFID, "runStateKind", sn.RunState.Kind)
		return intr.Processed()
	}

	dispatchCurrentEventAfterStore := d.Kind == ext.DirectiveKindEvent &&
		sn.RunState.Kind == ext.RunStateWaitEvent &&
		sn.Event.ID == ""

	updates, err := newUpdateFactory().BuildUpdates(sn, d)
	if err != nil {
		if errors.Is(err, ErrStaleDirective) {
			slog.Debug("dropping stale step timeout directive", "directiveID", d.ID, "wfID", d.RunInfo.WFID)
			return intr.Processed()
		}
		slog.Error("failed to build state updates", "error", err, "directiveID", d.ID)
		return dh.applyFailure(ctx, d, sn.RunState, err)
	}

	result, err := dh.applyStateUpdates(ctx, updates)
	if err != nil {
		slog.Error("failed to apply state updates", "error", err, "directiveID", d.ID)
		return dh.applyFailure(ctx, d, sn.RunState, err)
	}

	if dispatchCurrentEventAfterStore && result.IsProcessed() {
		return dh.dispatchInboxHead(ctx, d.RunInfo.WFID, d.RunInfo.ID)
	}

	return result
}

func (dh *DirectiveHandler) applyFailure(ctx context.Context, d ext.Directive, currentState ext.RunState, cause error) intr.HandleResult {
	failureUpdates, err := newUpdateFactory().BuildUnexpectedFailure(d, cause.Error(), currentState)
	if err != nil {
		slog.Error("failed to build failure updates", "error", err)
		return intr.Processed()
	}

	// If CAS rejection happens at that point, applyFailure will retry the failure updates.
	// But concrete failure will be logged and the result will be processed.
	result, err := dh.applyStateUpdates(ctx, failureUpdates)
	if err != nil {
		slog.Error("failed to apply failure updates", "error", err, "directiveID", d.ID)
		return intr.Processed()
	}
	return result
}

func (dh *DirectiveHandler) applyStateUpdates(ctx context.Context, updates []store.StateUpdate) (intr.HandleResult, error) {
	err := dh.store.ApplyStateUpdates(ctx, updates)
	if err != nil {
		if store.IsCASRejection(err) {
			// A CAS rejection, a concurrent update to the run state,
			// which could be due to another directive being processed at the same time.
			// Retrying after a delay allows the system to resolve the conflict and apply the updates successfully.
			return intr.Retryable(NackDelay), nil
		}

		if store.IsDuplicateMessage(err) {
			// Idempotent handling: if it's a duplicate message, we can consider it processed successfully.
			return intr.Processed(), nil
		}

		return intr.HandleResult{}, err
	}

	return intr.Processed(), nil
}

func (dh *DirectiveHandler) dispatchInboxHead(ctx context.Context, wfID ext.WFID, runID ext.RunID) intr.HandleResult {
	sn, err := dh.store.GetStateSnapshot(ctx, wfID, runID)
	if err != nil {
		return intr.Retryable(NackDelay)
	}

	if sn.RunState.Kind != ext.RunStateWaitEvent || sn.Event.ID == "" {
		return intr.Processed()
	}

	updates, err := newUpdateFactory().StartStep(sn.Event, sn.RunState)
	if err != nil {
		slog.Error("failed to build updates for waiting inbox event dispatch", "error", err, "wfID", wfID, "runID", runID)
		return dh.applyFailure(ctx, sn.Event, sn.RunState, err)
	}

	if event, ok := sn.Event.Msg.(*ext.Event); ok && event.EventSeqID != nil {
		inboxTask, err := ext.NewDeleteInboxEventTask(sn.Event.ID, *event.EventSeqID)
		if err != nil {
			slog.Error("failed to create inbox cleanup task for waiting dispatch", "error", err, "wfID", wfID, "runID", runID)
			return dh.applyFailure(ctx, sn.Event, sn.RunState, err)
		}
		updates = append(updates, store.BackgroundTaskUpdate{Task: inboxTask})
	}

	result, err := dh.applyStateUpdates(ctx, updates)
	if err != nil {
		slog.Error("failed to apply waiting inbox event dispatch updates", "error", err, "wfID", wfID, "runID", runID)
		return dh.applyFailure(ctx, sn.Event, sn.RunState, err)
	}

	return result
}
