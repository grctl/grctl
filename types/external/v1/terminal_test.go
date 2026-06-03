package external

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestRunStatusTerminated_IsTerminal(t *testing.T) {
	require.True(t, RunStatusTerminated.IsTerminal())
}

func TestRunStateTerminate_IsTerminal(t *testing.T) {
	s := RunState{Kind: RunStateTerminate}
	require.True(t, s.IsTerminal())
}

func TestWorkerTerminateRunCmd_RoundTrip(t *testing.T) {
	runID := NewRunID()
	cmd := Command{
		ID:        NewCmdID(),
		Kind:      CmdKindWorkerTerminateRun,
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		SenderID:  "server",
		Msg:       &WorkerTerminateRunCmd{RunID: runID},
	}

	data, err := msgpack.Marshal(&cmd)
	require.NoError(t, err)

	var decoded Command
	require.NoError(t, msgpack.Unmarshal(data, &decoded))

	require.Equal(t, cmd.ID, decoded.ID)
	require.Equal(t, CmdKindWorkerTerminateRun, decoded.Kind)
	require.True(t, cmd.Timestamp.Equal(decoded.Timestamp))

	terminate, ok := decoded.Msg.(*WorkerTerminateRunCmd)
	require.True(t, ok, "decoded message must be *WorkerTerminateRunCmd")
	require.Equal(t, runID, terminate.RunID)
}
