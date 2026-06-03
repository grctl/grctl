package run

import (
	"context"
	"testing"
	"time"

	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/require"
)

func TestStepPickedUp(t *testing.T) {
	ctx := context.Background()

	t.Run("updates WorkerID and writes step.started history", func(t *testing.T) {
		workerID := ext.WorkerID("worker-abc")
		d := stepPickedUpDirective("step-a", workerID)

		records, err := plan(ctx, d, stepSnapshot("some-directive"), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindStepStarted)
		state := rs.finalRunState()
		require.Equal(t, ext.RunStateStep, state.Kind)
		require.NotNil(t, state.WorkerID)
		require.Equal(t, workerID, *state.WorkerID)
	})

	t.Run("is silently dropped for a terminal run", func(t *testing.T) {
		d := stepPickedUpDirective("step-a", ext.WorkerID("w"))

		for _, kind := range []ext.RunStateKind{ext.RunStateComplete, ext.RunStateFail, ext.RunStateCancel, ext.RunStateTerminate} {
			records, err := plan(ctx, d, terminalSnapshot(kind), 0, 0)
			require.NoError(t, err)
			require.Nil(t, records)
		}
	})
}

// TestStep is the behaviour map for a workflow's steps: how a run enters its first
// step, advances between steps, and how a step's failure or timeout ends the run.
// Each case drives plan and asserts only on the records it emits.
func TestStep(t *testing.T) {
	ctx := context.Background()

	t.Run("a workflow starts by running its first step", func(t *testing.T) {
		records, err := plan(ctx, startDirective(nil), emptySnapshot(), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindRunStarted)
		rs.requireDispatchedStep("start")
		rs.requireTimer(ext.TimerKindStepTimeout)
		require.Equal(t, ext.RunStateStep, rs.finalRunState().Kind)
	})

	t.Run("a completed step advances the workflow to its next step", func(t *testing.T) {
		d := stepResultDirective("step-a", ext.DirectiveKindStep, &ext.Step{Name: "step-b"})

		records, err := plan(ctx, d, stepSnapshot("directive-of-step-a"), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindStepCompleted)
		rs.requireBgTask(ext.BackgroundTaskKindDeleteTimer)
		rs.requireDispatchedStep("step-b")
		rs.requireTimer(ext.TimerKindStepTimeout)
		require.Equal(t, ext.RunStateStep, rs.finalRunState().Kind)
	})

	t.Run("a failed step fails the workflow", func(t *testing.T) {
		failErr := ext.ErrorDetails{Type: "ValueError", Message: "step blew up"}
		d := stepResultDirective("step-a", ext.DirectiveKindFailStep, &ext.FailStep{StepName: "step-a", Error: failErr})

		records, err := plan(ctx, d, stepSnapshot("directive-of-step-a"), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindStepFailed)
		rs.requireHistory(ext.HistoryKindRunFailed)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, ext.RunStateFail, rs.finalRunState().Kind)
		require.Equal(t, failErr, rs.requireRunError())
	})

	t.Run("completing a step does not set WorkerID on the next run state", func(t *testing.T) {
		workerID := ext.WorkerID("worker-xyz")
		d := ext.Directive{
			ID:        ext.NewDirectiveID(),
			Timestamp: time.Now().UTC(),
			Kind:      ext.DirectiveKindStepResult,
			RunInfo:   testRunInfo(),
			Msg: &ext.StepResult{
				ProcessedMsgKind: ext.DirectiveKindStep,
				ProcessedMsg:     &ext.Step{Name: "step-a"},
				WorkerID:         &workerID,
				NextMsgKind:      ext.DirectiveKindStep,
				NextMsg:          &ext.Step{Name: "step-b"},
			},
		}

		records, err := plan(ctx, d, stepSnapshot("directive-of-step-a"), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		require.Nil(t, rs.finalRunState().WorkerID)
	})

	t.Run("a step that runs past its timeout fails the workflow", func(t *testing.T) {
		stepDirectiveID := ext.NewDirectiveID()
		d := stepTimeoutDirective("step-a", stepDirectiveID)

		records, err := plan(ctx, d, stepSnapshot(stepDirectiveID), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindStepTimeout)
		rs.requireHistory(ext.HistoryKindRunFailed)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, ext.RunStateFail, rs.finalRunState().Kind)
		require.Equal(t, "StepTimeout", rs.requireRunError().Type)
	})

	t.Run("a step timeout sends WorkerTerminateRun to the executing worker", func(t *testing.T) {
		workerID := ext.WorkerID("worker-abc")
		stepDirectiveID := ext.NewDirectiveID()
		d := stepTimeoutDirective("step-a", stepDirectiveID)

		records, err := plan(ctx, d, stepSnapshotWithWorker(stepDirectiveID, workerID), 0, 0)
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		payload := rs.requireWorkerTerminateRun()
		require.Equal(t, workerID, payload.WorkerID)
		require.Equal(t, testRunID, payload.RunID)
	})
}
