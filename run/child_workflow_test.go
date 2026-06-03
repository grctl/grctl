package run

import (
	"context"
	"testing"

	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/require"
)

// TestChildWorkflow maps how a child run reports its terminal outcome back to the
// parent that started it. The child reaches a terminal state through its own worker
// (complete / fail) or a cancel request; plan then emits a single notify-parent task
// carrying the outcome to the parent's callback step. A run without a parent emits none.
func TestChildWorkflow(t *testing.T) {
	ctx := context.Background()

	const (
		parentWFID   ext.WFID = "parent-wf"
		callbackStep string   = "on_child_done"
	)

	t.Run("a completed child workflow notifies its parent with the result", func(t *testing.T) {
		d := withParentCallback(
			stepResultDirective("final-step", ext.DirectiveKindComplete, &ext.Complete{Result: "child-output"}),
			parentWFID, callbackStep,
		)

		records, err := plan(ctx, d, stepSnapshot("directive-of-final-step"), 0, 0)
		require.NoError(t, err)

		payload := newRecordSet(t, records).requireParentNotification()
		require.Equal(t, parentWFID, payload.ParentWFID)
		require.Equal(t, callbackStep, payload.StepName)
		require.Equal(t, testWFID, payload.ChildWFID)
		require.Equal(t, ext.RunStatusCompleted, payload.Status)
		require.Equal(t, "child-output", payload.Result)
		require.Nil(t, payload.Error)
	})

	t.Run("a failed child workflow notifies its parent with the error", func(t *testing.T) {
		failMsg := &ext.Fail{Error: ext.ErrorDetails{Type: "RuntimeError", Message: "child blew up"}}
		d := withParentCallback(
			stepResultDirective("risky-step", ext.DirectiveKindFail, failMsg),
			parentWFID, callbackStep,
		)

		records, err := plan(ctx, d, stepSnapshot("directive-of-risky-step"), 0, 0)
		require.NoError(t, err)

		payload := newRecordSet(t, records).requireParentNotification()
		require.Equal(t, ext.RunStatusFailed, payload.Status)
		require.Nil(t, payload.Result)
		require.NotNil(t, payload.Error)
		require.Equal(t, "RuntimeError", payload.Error.Type)
	})

	t.Run("a cancelled child workflow notifies its parent when cancel is immediate (outside a step)", func(t *testing.T) {
		d := withParentCallback(cancelDirective("parent asked to stop"), parentWFID, callbackStep)

		records, err := plan(ctx, d, waitSnapshot(), 0, 0)
		require.NoError(t, err)

		payload := newRecordSet(t, records).requireParentNotification()
		require.Equal(t, ext.RunStatusCancelled, payload.Status)
		require.NotNil(t, payload.Error)
		require.Equal(t, "parent asked to stop", payload.Error.Message)
	})

	t.Run("a cancelled child workflow notifies its parent when cancel is deferred (step finishes after cancel)", func(t *testing.T) {
		pendingCancel := withParentCallback(cancelDirective("parent asked to stop"), parentWFID, callbackStep)
		sn := stepSnapshotWithPendingCancel("step-directive", pendingCancel)

		d := stepResultDirective("my-step", ext.DirectiveKindStep, &ext.Step{Name: "next-step"})
		records, err := plan(ctx, d, sn, 0, 0)
		require.NoError(t, err)

		payload := newRecordSet(t, records).requireParentNotification()
		require.Equal(t, ext.RunStatusCancelled, payload.Status)
		require.NotNil(t, payload.Error)
		require.Equal(t, "parent asked to stop", payload.Error.Message)
	})

	t.Run("a workflow with no parent produces no parent notification", func(t *testing.T) {
		d := stepResultDirective("final-step", ext.DirectiveKindComplete, &ext.Complete{Result: "output"})

		records, err := plan(ctx, d, stepSnapshot("directive-of-final-step"), 0, 0)
		require.NoError(t, err)

		newRecordSet(t, records).requireNoBgTask(ext.BackgroundTaskKindNotifyParentComplete)
	})
}
