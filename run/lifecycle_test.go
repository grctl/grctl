package run

import (
	"context"
	"testing"

	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/require"
)

// TestWorkflowLifecycle maps how a run reaches a terminal state through its steps.
func TestWorkflowLifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("a workflow completes when its final step finishes", func(t *testing.T) {
		d := stepResultDirective("final-step", ext.DirectiveKindComplete, &ext.Complete{Result: "done"})

		records, err := plan(ctx, d, stepSnapshot("directive-of-final-step"))
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindStepCompleted)
		rs.requireHistory(ext.HistoryKindRunCompleted)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, "done", rs.requireRunOutput())
		require.Equal(t, ext.RunStateComplete, rs.finalRunState().Kind)
	})

	t.Run("a workflow can fail the run after a step completes", func(t *testing.T) {
		failErr := &ext.Fail{Error: ext.ErrorDetails{Type: "RuntimeError", Message: "unrecoverable error"}}
		d := stepResultDirective("risky-step", ext.DirectiveKindFail, failErr)

		records, err := plan(ctx, d, stepSnapshot("directive-of-risky-step"))
		require.NoError(t, err)

		// The step itself ran to completion; failing the run is a separate
		// workflow-author decision (ctx.fail), so both events are recorded.
		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindStepCompleted)
		rs.requireHistory(ext.HistoryKindRunFailed)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, ext.RunStateFail, rs.finalRunState().Kind)
		require.Equal(t, "RuntimeError", rs.requireRunError().Type)
	})

	t.Run("a running workflow is cancelled on request", func(t *testing.T) {
		d := cancelDirective("operator stopped it")

		records, err := plan(ctx, d, stepSnapshot("directive-of-current-step"))
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindRunCancelled)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, ext.RunStateCancel, rs.finalRunState().Kind)
	})
}

// TestTerminate maps what happens when an operator forcefully terminates a run.
func TestTerminate(t *testing.T) {
	ctx := context.Background()

	t.Run("terminate transitions the run to terminated state", func(t *testing.T) {
		d := terminateDirective("operator killed it")

		records, err := plan(ctx, d, stepSnapshot("directive-of-current-step"))
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindRunTerminated)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, ext.RunStateTerminate, rs.finalRunState().Kind)
	})

	t.Run("terminate sends WorkerTerminateRun when a worker is executing the active step", func(t *testing.T) {
		workerID := ext.WorkerID("worker-xyz")
		activeID := ext.NewDirectiveID()
		d := terminateDirective("urgent stop")

		records, err := plan(ctx, d, stepSnapshotWithWorker(activeID, workerID))
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		payload := rs.requireWorkerTerminateRun()
		require.Equal(t, workerID, payload.WorkerID)
		require.Equal(t, testRunID, payload.RunID)
	})

	t.Run("terminate does not send WorkerTerminateRun when no worker is known", func(t *testing.T) {
		d := terminateDirective("no worker yet")

		records, err := plan(ctx, d, stepSnapshot("directive-of-current-step"))
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireNoBgTask(ext.BackgroundTaskKindWorkerTerminateRun)
	})

	t.Run("terminate is silently dropped for an already-terminal run", func(t *testing.T) {
		d := terminateDirective("too late")

		for _, kind := range []ext.RunStateKind{ext.RunStateComplete, ext.RunStateFail, ext.RunStateCancel, ext.RunStateTerminate} {
			records, err := plan(ctx, d, terminalSnapshot(kind))
			require.NoError(t, err)
			require.Nil(t, records)
		}
	})
}

// TestFinishedRun maps the guard that a run which has already finished produces no
// further effects, whatever stimulus arrives late.
func TestFinishedRun(t *testing.T) {
	ctx := context.Background()

	finished := []struct {
		name string
		kind ext.RunStateKind
	}{
		{"completed", ext.RunStateComplete},
		{"failed", ext.RunStateFail},
		{"cancelled", ext.RunStateCancel},
	}

	for _, tc := range finished {
		t.Run("a "+tc.name+" run ignores a late event", func(t *testing.T) {
			records, err := plan(ctx, eventDirective("late-signal", nil), terminalSnapshot(tc.kind))

			require.NoError(t, err)
			require.Empty(t, records)
		})
	}
}
