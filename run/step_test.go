package run

import (
	"context"
	"testing"

	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/require"
)

// TestStep is the behaviour map for a workflow's steps: how a run enters its first
// step, advances between steps, and how a step's failure or timeout ends the run.
// Each case drives plan and asserts only on the records it emits.
func TestStep(t *testing.T) {
	ctx := context.Background()

	t.Run("a workflow starts by running its first step", func(t *testing.T) {
		records, err := plan(ctx, startDirective(nil), emptySnapshot())
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindRunStarted)
		rs.requireDispatchedStep("start")
		rs.requireTimer(ext.TimerKindStepTimeout)
		require.Equal(t, ext.RunStateStep, rs.finalRunState().Kind)
	})

	t.Run("a completed step advances the workflow to its next step", func(t *testing.T) {
		d := stepResultDirective("step-a", ext.DirectiveKindStep, &ext.Step{Name: "step-b"})

		records, err := plan(ctx, d, stepSnapshot("directive-of-step-a"))
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

		records, err := plan(ctx, d, stepSnapshot("directive-of-step-a"))
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindStepFailed)
		rs.requireHistory(ext.HistoryKindRunFailed)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, ext.RunStateFail, rs.finalRunState().Kind)
		require.Equal(t, failErr, rs.requireRunError())
	})

	t.Run("a step that runs past its timeout fails the workflow", func(t *testing.T) {
		stepDirectiveID := ext.NewDirectiveID()
		d := stepTimeoutDirective("step-a", stepDirectiveID)

		records, err := plan(ctx, d, stepSnapshot(stepDirectiveID))
		require.NoError(t, err)

		rs := newRecordSet(t, records)
		rs.requireHistory(ext.HistoryKindStepTimeout)
		rs.requireHistory(ext.HistoryKindRunFailed)
		rs.requireBgTask(ext.BackgroundTaskKindPurgeRunResidue)
		require.Equal(t, ext.RunStateFail, rs.finalRunState().Kind)
		require.Equal(t, "StepTimeout", rs.requireRunError().Type)
	})
}
