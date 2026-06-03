package run

import (
	"context"
	"testing"

	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/require"
)

// TestCancel is the behaviour map for step-aware graceful cancellation.
// A cancel directive defers when a step is in progress and executes immediately otherwise.
func TestCancel(t *testing.T) {
	ctx := context.Background()

	t.Run("cancel during step emits CancelReceived history and sets PendingCancel", func(t *testing.T) {
		d := cancelDirective("stop the run")

		records, err := plan(ctx, d, stepSnapshot("directive-of-current-step"), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindRunCancelReceived)
		pending := rs.requirePendingCancel()
		require.Equal(t, d.ID, pending.ID)
		rs.requireNoTerminalRunState()
	})

	t.Run("cancel during step does not produce terminal records", func(t *testing.T) {
		d := cancelDirective("stop")

		records, err := plan(ctx, d, stepSnapshot("step-directive"), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireNoTerminalRunState()
		for _, r := range records {
			require.IsNotType(t, nil, r) // sanity; real assertion is requireNoTerminalRunState
		}
	})

	t.Run("cancel during wait cancels the run immediately", func(t *testing.T) {
		d := cancelDirective("stop now")

		records, err := plan(ctx, d, waitSnapshot(), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindRunCancelled)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, ext.RunStateCancel, rs.finalRunState().Kind)
	})

	t.Run("cancel when run is already terminal produces no records", func(t *testing.T) {
		d := cancelDirective("too late")

		for _, kind := range []ext.RunStateKind{ext.RunStateComplete, ext.RunStateFail, ext.RunStateCancel, ext.RunStateTerminate} {
			records, err := plan(ctx, d, terminalSnapshot(kind), 0, 0)
			require.NoError(t, err)
			require.Nil(t, records)
		}
	})

	t.Run("second cancel during step overwrites PendingCancel", func(t *testing.T) {
		firstCancel := cancelDirective("first stop")
		secondCancel := cancelDirective("second stop")

		sn := stepSnapshotWithPendingCancel("step-directive", firstCancel)

		records, err := plan(ctx, secondCancel, sn, 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindRunCancelReceived)
		pending := rs.requirePendingCancel()
		require.Equal(t, secondCancel.ID, pending.ID)
		rs.requireNoTerminalRunState()
	})
}

// TestPendingCancelOnStepResult is the behaviour map for how a PendingCancel in RunState
// is resolved when a StepResult arrives.
func TestPendingCancelOnStepResult(t *testing.T) {
	ctx := context.Background()

	t.Run("successful step result with PendingCancel transitions run to cancelled", func(t *testing.T) {
		pendingCancel := cancelDirective("stop after step")
		sn := stepSnapshotWithPendingCancel("step-directive", pendingCancel)

		d := stepResultDirective("my-step", ext.DirectiveKindStep, &ext.Step{Name: "next-step"})
		records, err := plan(ctx, d, sn, 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindRunCancelled)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, ext.RunStateCancel, rs.finalRunState().Kind)
	})

	t.Run("failed step result with PendingCancel fails the run, not cancels", func(t *testing.T) {
		pendingCancel := cancelDirective("stop after step")
		sn := stepSnapshotWithPendingCancel("step-directive", pendingCancel)

		failErr := ext.ErrorDetails{Type: "ValueError", Message: "step blew up"}
		d := stepResultDirective("my-step", ext.DirectiveKindFailStep, &ext.FailStep{StepName: "my-step", Error: failErr})

		records, err := plan(ctx, d, sn, 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindStepFailed)
		rs.requireHistory(ext.HistoryKindRunFailed)
		require.Equal(t, ext.RunStateFail, rs.finalRunState().Kind)
		require.Equal(t, failErr, rs.requireRunError())
	})

	t.Run("step result with PendingCancel does not advance to next step", func(t *testing.T) {
		pendingCancel := cancelDirective("stop")
		sn := stepSnapshotWithPendingCancel("step-directive", pendingCancel)

		d := stepResultDirective("my-step", ext.DirectiveKindStep, &ext.Step{Name: "next-step"})
		records, err := plan(ctx, d, sn, 0, 0)
		require.NoError(t, err)

		for _, r := range records {
			if rec, ok := r.(interface{ StepName() string }); ok {
				require.NotEqual(t, "next-step", rec.StepName(), "next step must not be dispatched when cancelling")
			}
		}
	})
}
