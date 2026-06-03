package record

import (
	"testing"
	"time"

	model "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/require"
)

func testCancelDirective() ext.Directive {
	started := time.Now().UTC()
	ri := ext.RunInfo{
		ID:        ext.RunID("run-test"),
		WFID:      ext.WFID("wf-test"),
		WFType:    "test-workflow",
		Status:    ext.RunStatusRunning,
		CreatedAt: started,
		StartedAt: &started,
	}
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindCancel,
		RunInfo:   ri,
		Msg:       &ext.Cancel{Reason: "test reason"},
	}
}

func testRunState() ext.RunState {
	entered := time.Now().UTC()
	return ext.RunState{
		WFID:      ext.WFID("wf-test"),
		RunID:     ext.RunID("run-test"),
		Kind:      ext.RunStateStep,
		EnteredAt: entered,
		SeqID:     42,
	}
}

func TestCancelReceived(t *testing.T) {
	t.Run("emits RunCancelReceived history event", func(t *testing.T) {
		d := testCancelDirective()
		currentState := testRunState()

		records, err := CancelReceived(d, currentState)
		require.NoError(t, err)

		var found bool
		for _, r := range records {
			if rec, ok := r.(model.HistoryRecord); ok && rec.History.Kind == ext.HistoryKindRunCancelReceived {
				found = true
			}
		}
		require.True(t, found, "expected RunCancelReceived history event")
	})

	t.Run("emits RunStateRecord with PendingCancel set", func(t *testing.T) {
		d := testCancelDirective()
		currentState := testRunState()

		records, err := CancelReceived(d, currentState)
		require.NoError(t, err)

		var stateRec model.RunStateRecord
		var found bool
		for _, r := range records {
			if rec, ok := r.(model.RunStateRecord); ok {
				stateRec = rec
				found = true
			}
		}
		require.True(t, found, "expected RunStateRecord")
		require.NotNil(t, stateRec.State.PendingCancel, "PendingCancel must be set")
		require.Equal(t, d.ID, stateRec.State.PendingCancel.ID)
		require.Equal(t, currentState.SeqID, stateRec.ExpectedSeq)
	})

	t.Run("does not emit an InboxRecord", func(t *testing.T) {
		d := testCancelDirective()
		currentState := testRunState()

		records, err := CancelReceived(d, currentState)
		require.NoError(t, err)

		for _, r := range records {
			_, isInbox := r.(model.InboxRecord)
			require.False(t, isInbox, "CancelReceived must not emit an InboxRecord")
		}
	})
}
