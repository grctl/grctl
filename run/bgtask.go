package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"grctl/server/metrics"
	model "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/vmihailenco/msgpack/v5"
)

type TimerCanceler interface {
	CancelTimer(ctx context.Context, wfID ext.WFID, kind ext.TimerKind, timerID ext.TimerID) error
}

type InboxEventDeleter interface {
	DeleteInboxEvent(ctx context.Context, seqID uint64) error
}

type RunResiduePurger interface {
	PurgeRunResidue(ctx context.Context, wfID ext.WFID) error
}

// ParentNotifier delivers a child's terminal outcome to its parent by publishing an
// event directive to the parent run. It mirrors what the API Send path does, reused
// here so a completed child can wake its parent's callback step.
type ParentNotifier interface {
	GetRunByWFID(ctx context.Context, wfID ext.WFID) (ext.RunInfo, uint64, error)
	PublishDirective(ctx context.Context, d ext.Directive) error
}

// WorkerCommandPublisher sends a command to a specific worker over request-reply.
// Returns model.ErrWorkerUnreachable if the worker is gone or does not respond.
type WorkerCommandPublisher interface {
	PublishWorkerCommand(workerID string, cmd ext.Command) error
}

type BgTaskHandler struct {
	timers         TimerCanceler
	inbox          InboxEventDeleter
	residuePurger  RunResiduePurger
	parentNotifier ParentNotifier
	workerCmds     WorkerCommandPublisher
	metrics        metrics.Recorder
	maxDeliveries  uint64
}

func NewBgTaskHandler(
	timers TimerCanceler,
	inbox InboxEventDeleter,
	residuePurger RunResiduePurger,
	parentNotifier ParentNotifier,
	workerCmds WorkerCommandPublisher,
	metricsRecorder metrics.Recorder,
	maxDeliveries uint64,
) *BgTaskHandler {
	return &BgTaskHandler{
		timers:         timers,
		inbox:          inbox,
		residuePurger:  residuePurger,
		parentNotifier: parentNotifier,
		workerCmds:     workerCmds,
		metrics:        metricsRecorder,
		maxDeliveries:  maxDeliveries,
	}
}

func (h *BgTaskHandler) Handle(ctx context.Context, task ext.BackgroundTask, numDelivered uint64) model.HandleResult {
	if numDelivered > h.maxDeliveries {
		slog.Warn("background task exceeded max deliveries, discarding",
			"kind", task.Kind,
			"num_delivered", numDelivered,
			"max_deliveries", h.maxDeliveries,
		)
		h.metrics.RecordBgTask(ctx, string(task.Kind), "discarded")
		return model.Processed()
	}

	var result model.HandleResult
	switch task.Kind {
	case ext.BackgroundTaskKindDeleteTimer:
		result = h.handleDeleteTimer(ctx, task)
	case ext.BackgroundTaskKindDeleteInboxEvent:
		result = h.handleDeleteInboxEvent(ctx, task)
	case ext.BackgroundTaskKindPurgeRunResidue:
		result = h.handlePurgeRunResidue(ctx, task)
	case ext.BackgroundTaskKindNotifyParentComplete:
		result = h.handleNotifyParentComplete(ctx, task)
	case ext.BackgroundTaskKindWorkerTerminateRun:
		result = h.handleWorkerTerminateRun(ctx, task)
	default:
		slog.Warn("unknown background task kind, discarding", "kind", task.Kind)
		h.metrics.RecordBgTask(ctx, string(task.Kind), "discarded")
		return model.Processed()
	}

	resultLabel := "processed"
	if result.Action == model.ActionRetryable {
		resultLabel = "retried"
	}
	h.metrics.RecordBgTask(ctx, string(task.Kind), resultLabel)
	return result
}

func (h *BgTaskHandler) handleDeleteTimer(ctx context.Context, task ext.BackgroundTask) model.HandleResult {
	var payload ext.DeleteTimerPayload
	if err := msgpack.Unmarshal(task.Payload, &payload); err != nil {
		slog.Error("failed to unmarshal delete timer payload, discarding",
			"error", err,
		)
		return model.Processed()
	}

	if err := h.timers.CancelTimer(ctx, payload.WFID, payload.Kind, payload.TimerID); err != nil {
		slog.Warn("failed to cancel timer, will retry",
			"wf_id", payload.WFID,
			"kind", payload.Kind,
			"timer_id", payload.TimerID,
			"error", err,
		)
		return model.Retryable(RetryDelay)
	}

	slog.Debug("timer deleted by background task",
		"wf_id", payload.WFID,
		"kind", payload.Kind,
		"timer_id", payload.TimerID,
	)
	return model.Processed()
}

func (h *BgTaskHandler) handleDeleteInboxEvent(ctx context.Context, task ext.BackgroundTask) model.HandleResult {
	var payload ext.DeleteInboxEventPayload
	if err := msgpack.Unmarshal(task.Payload, &payload); err != nil {
		slog.Error("failed to unmarshal delete inbox event payload, discarding",
			"deduplication_id", task.DeduplicationID,
			"error", err,
		)
		return model.Processed()
	}

	if err := h.inbox.DeleteInboxEvent(ctx, payload.SeqID); err != nil {
		slog.Warn("failed to delete inbox event, will retry",
			"seq_id", payload.SeqID,
			"error", fmt.Sprintf("%v", err),
		)
		return model.Retryable(RetryDelay)
	}

	slog.Debug("inbox event deleted by background task", "seq_id", payload.SeqID)
	return model.Processed()
}

func (h *BgTaskHandler) handlePurgeRunResidue(ctx context.Context, task ext.BackgroundTask) model.HandleResult {
	var payload ext.PurgeRunResiduePayload
	if err := msgpack.Unmarshal(task.Payload, &payload); err != nil {
		slog.Error("failed to unmarshal purge run residue payload, discarding",
			"deduplication_id", task.DeduplicationID,
			"error", err,
		)
		return model.Processed()
	}

	if err := h.residuePurger.PurgeRunResidue(ctx, payload.WFID); err != nil {
		slog.Warn("failed to purge run residue, will retry",
			"wf_id", payload.WFID,
			"error", err,
		)
		return model.Retryable(RetryDelay)
	}

	slog.Debug("run residue purged by background task", "wf_id", payload.WFID)
	return model.Processed()
}

func (h *BgTaskHandler) handleNotifyParentComplete(ctx context.Context, task ext.BackgroundTask) model.HandleResult {
	var payload ext.NotifyParentCompletePayload
	if err := msgpack.Unmarshal(task.Payload, &payload); err != nil {
		slog.Error("failed to unmarshal notify parent complete payload, discarding",
			"deduplication_id", task.DeduplicationID,
			"error", err,
		)
		return model.Processed()
	}

	parent, _, err := h.parentNotifier.GetRunByWFID(ctx, payload.ParentWFID)
	if err != nil {
		slog.Warn("failed to load parent run for completion callback, will retry",
			"parentWFID", payload.ParentWFID,
			"childWFID", payload.ChildWFID,
			"error", err,
		)
		return model.Retryable(RetryDelay)
	}

	// A parent that already finished before its child's callback arrived is a workflow
	// design issue, not an engine fault: the callback step has nowhere to run. Drop it
	// loudly so an operator can spot the dangling child instead of retrying forever.
	if parent.Status.IsTerminal() {
		slog.Warn("parent run already terminal, dropping child completion callback",
			"parentWFID", payload.ParentWFID,
			"parentStatus", parent.Status,
			"childWFID", payload.ChildWFID,
			"stepName", payload.StepName,
		)
		return model.Processed()
	}

	// Deterministic ID derived from the task's dedup ID guards against the bg-task
	// queue's at-least-once redelivery: a republish carries the same Nats-Msg-Id, which
	// JetStream drops within its duplicate window. Retries are far quicker than that
	// window, so in practice the parent sees the callback exactly once.
	directive := ext.Directive{
		ID:        ext.DirectiveID("notify." + string(task.DeduplicationID)),
		Kind:      ext.DirectiveKindEvent,
		Timestamp: time.Now().UTC(),
		RunInfo:   parent,
		Msg: &ext.Event{
			EventName: payload.StepName,
			Payload: map[string]any{
				"status": payload.Status,
				"result": payload.Result,
				"error":  payload.Error,
			},
		},
	}

	if err := h.parentNotifier.PublishDirective(ctx, directive); err != nil {
		slog.Warn("failed to publish completion callback to parent, will retry",
			"parentWFID", payload.ParentWFID,
			"childWFID", payload.ChildWFID,
			"stepName", payload.StepName,
			"error", err,
		)
		return model.Retryable(RetryDelay)
	}

	slog.Debug("delivered child completion callback to parent",
		"parentWFID", payload.ParentWFID,
		"childWFID", payload.ChildWFID,
		"stepName", payload.StepName,
		"status", payload.Status,
	)
	return model.Processed()
}

func (h *BgTaskHandler) handleWorkerTerminateRun(_ context.Context, task ext.BackgroundTask) model.HandleResult {
	var payload ext.WorkerTerminateRunPayload
	if err := msgpack.Unmarshal(task.Payload, &payload); err != nil {
		slog.Error("failed to unmarshal worker terminate run payload, discarding",
			"error", err,
		)
		return model.Processed()
	}

	cmd := ext.Command{
		ID:        ext.NewCmdID(),
		Kind:      ext.CmdKindWorkerTerminateRun,
		Timestamp: time.Now().UTC(),
		Msg:       &ext.WorkerTerminateRunCmd{RunID: payload.RunID},
	}

	if err := h.workerCmds.PublishWorkerCommand(string(payload.WorkerID), cmd); err != nil {
		if errors.Is(err, model.ErrWorkerUnreachable) {
			slog.Debug("worker unreachable for terminate signal, it already stopped",
				"workerID", payload.WorkerID,
				"run_id", payload.RunID,
			)
			return model.Processed()
		}
		slog.Warn("failed to send terminate to worker, will retry",
			"workerID", payload.WorkerID,
			"run_id", payload.RunID,
			"error", err,
		)
		return model.Retryable(RetryDelay)
	}

	slog.Debug("sent terminate signal to worker",
		"workerID", payload.WorkerID,
		"run_id", payload.RunID,
	)
	return model.Processed()
}
