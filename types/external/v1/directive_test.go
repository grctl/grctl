package external

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestStepPickedUp_MsgpackRoundTrip(t *testing.T) {
	workerID := WorkerID("worker-abc")
	ts := time.Now().UTC().Truncate(time.Millisecond)

	original := Directive{
		ID:        NewDirectiveID(),
		Timestamp: ts,
		Kind:      DirectiveKindStepPickedUp,
		RunInfo: RunInfo{
			ID:   RunID("run-1"),
			WFID: WFID("wf-1"),
		},
		Msg: &StepPickedUp{
			StepName:  "my-step",
			WorkerID:  workerID,
			Timestamp: ts,
		},
	}

	data, err := msgpack.Marshal(&original)
	require.NoError(t, err)

	var decoded Directive
	require.NoError(t, msgpack.Unmarshal(data, &decoded))

	require.Equal(t, DirectiveKindStepPickedUp, decoded.Kind)
	msg, ok := decoded.Msg.(*StepPickedUp)
	require.True(t, ok, "expected *StepPickedUp, got %T", decoded.Msg)
	require.Equal(t, "my-step", msg.StepName)
	require.Equal(t, workerID, msg.WorkerID)
	require.True(t, ts.Equal(msg.Timestamp), "expected %v, got %v", ts, msg.Timestamp)
}
