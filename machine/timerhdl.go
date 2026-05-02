package machine

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	intr "grctl/server/types"
	ext "grctl/server/types/external/v1"
)

type DirectivePublisher interface {
	PublishDirective(ctx context.Context, d ext.Directive) error
}

type TimerMsgHandler struct {
	publisher     DirectivePublisher
	maxDeliveries uint64
}

func NewTimerMsgHandler(publisher DirectivePublisher, maxDeliveries uint64) *TimerMsgHandler {
	return &TimerMsgHandler{
		publisher:     publisher,
		maxDeliveries: maxDeliveries,
	}
}

func (h *TimerMsgHandler) Handle(ctx context.Context, timer ext.Timer, numDelivered uint64) intr.HandleResult {
	if timer.Directive.ID != "" {
		return h.handleDirectiveTimer(ctx, timer, numDelivered)
	}

	if timer.Command.ID != "" {
		// Command timers are not yet implemented.
		slog.Error("command timer fired but not supported",
			"timerID", timer.ID,
			"wfID", timer.WFID,
		)
		return intr.Processed()
	}

	// Timer has neither Directive nor Command — unrecoverable payload, no workflow to fail.
	slog.Error("timer fired with no payload, cannot identify workflow",
		"timerID", timer.ID,
		"wfID", timer.WFID,
	)
	return intr.Processed()
}

func (h *TimerMsgHandler) handleDirectiveTimer(ctx context.Context, timer ext.Timer, numDelivered uint64) intr.HandleResult {
	if numDelivered > h.maxDeliveries {
		slog.Error("timer exceeded max deliveries, failing workflow",
			"timerID", timer.ID,
			"wfID", timer.WFID,
			"numDelivered", numDelivered,
			"maxDeliveries", h.maxDeliveries,
		)
		cause := fmt.Sprintf("timer %s exceeded max delivery count %d", timer.ID, h.maxDeliveries)
		return h.applyFailure(ctx, timer.Directive, cause)
	}

	switch timer.Kind {
	case ext.TimerKindStepTimeout:
		return h.handleStepTimeout(ctx, timer)
	case ext.TimerKindWaitEventTimeout:
		return h.handleWaitEventTimeout(timer)
	case ext.TimerKindSleep:
		return h.handleSleepTimeout(timer)
	case ext.TimerKindSleepUntil:
		// SleepUntil is just a convenience for sleep timer with a datetime instead of a duration
		return h.handleSleepTimeout(timer)
	default:
		slog.Error("Unknown timer kind", "kind", timer.Kind, "wfID", timer.WFID)
		return intr.Processed()
	}
}

func (h *TimerMsgHandler) handleStepTimeout(ctx context.Context, timer ext.Timer) intr.HandleResult {
	directive, err := h.buildStepTimeoutDirective(timer)
	if err != nil {
		slog.Error("Failed to build step timeout directive", "error", err, "wfID", timer.WFID)
		cause := fmt.Sprintf("failed to build step timeout directive: %v", err)
		return h.applyFailure(ctx, timer.Directive, cause)
	}

	err = h.publisher.PublishDirective(ctx, directive)
	if err != nil {
		slog.Error("Failed to enqueue step timeout directive",
			"error", err,
			"wfID", timer.WFID,
			"runID", directive.RunInfo.ID,
			"directiveKind", directive.Kind,
		)
		return intr.Retryable(NackDelay)
	}

	slog.Debug("Enqueued step timeout directive", "wfID", timer.WFID)
	return intr.Processed()
}

func (h *TimerMsgHandler) buildStepTimeoutDirective(timer ext.Timer) (ext.Directive, error) {
	dispatchable, ok := timer.Directive.Msg.(ext.DispatchableMessage)
	if !ok {
		slog.Error("Invalid directive message for step timeout timer", "expected", "DispatchableMessage", "got", timer.Directive.Msg)
		return ext.Directive{}, fmt.Errorf("expected DispatchableMessage directive but got %T", timer.Directive.Msg)
	}
	runInfo := timer.Directive.RunInfo
	return ext.Directive{
		ID:      ext.NewDirectiveID(),
		Kind:    ext.DirectiveKindStepTimeout,
		RunInfo: runInfo,
		Msg: ext.StepTimeout{
			StepName:            dispatchable.StepName(),
			OriginalDirectiveID: timer.Directive.ID,
		},
	}, nil
}

func (h *TimerMsgHandler) applyFailure(ctx context.Context, d ext.Directive, cause string) intr.HandleResult {
	failDirective := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindFail,
		RunInfo:   d.RunInfo,
		Msg: &ext.Fail{
			Error: ext.ErrorDetails{
				Type:    "TimerExhausted",
				Message: cause,
			},
		},
	}

	if err := h.publisher.PublishDirective(ctx, failDirective); err != nil {
		slog.Error("failed to publish failure directive after timer exhaustion",
			"wfID", d.RunInfo.WFID,
			"error", err,
		)
		return intr.Retryable(NackDelay)
	}

	return intr.Processed()
}

func (h *TimerMsgHandler) handleWaitEventTimeout(_ ext.Timer) intr.HandleResult {
	return intr.HandleResult{}
}

func (h *TimerMsgHandler) handleSleepTimeout(_ ext.Timer) intr.HandleResult {
	return intr.HandleResult{}
}
