package external

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestRegisterCmd_RoundTrip(t *testing.T) {
	cmd := Command{
		ID:        NewCmdID(),
		Kind:      CmdKindWorkerRegister,
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		SenderID:  "w:abc123",
		Msg: &RegisterCmd{
			WorkerID: "w:abc123@host",
			Types: []WorkflowTypeDef{
				{
					Type:      WFType("order_wf"),
					StartStep: "start",
					Steps:     []string{"reserve", "charge"},
					Events:    []string{"approve"},
					Queries:   []string{"status"},
				},
				{
					Type:      WFType("payment_wf"),
					StartStep: "begin",
					Steps:     []string{"authorize"},
				},
			},
		},
	}

	data, err := msgpack.Marshal(&cmd)
	require.NoError(t, err)

	var decoded Command
	require.NoError(t, msgpack.Unmarshal(data, &decoded))

	require.Equal(t, cmd.ID, decoded.ID)
	require.Equal(t, CmdKindWorkerRegister, decoded.Kind)
	require.True(t, cmd.Timestamp.Equal(decoded.Timestamp))

	register, ok := decoded.Msg.(*RegisterCmd)
	require.True(t, ok, "decoded message must be *RegisterCmd")
	require.Equal(t, cmd.Msg, register)
}

func TestRegisterCmd_EmptyCatalogRoundTrip(t *testing.T) {
	cmd := Command{
		ID:        NewCmdID(),
		Kind:      CmdKindWorkerRegister,
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		SenderID:  "w:empty",
		Msg:       &RegisterCmd{WorkerID: "w:empty@host"},
	}

	data, err := msgpack.Marshal(&cmd)
	require.NoError(t, err)

	var decoded Command
	require.NoError(t, msgpack.Unmarshal(data, &decoded))

	register, ok := decoded.Msg.(*RegisterCmd)
	require.True(t, ok)
	require.Equal(t, "w:empty@host", register.WorkerID)
	require.Empty(t, register.Types)
}

func TestCommand_SenderID_RoundTrip(t *testing.T) {
	cmd := Command{
		ID:        NewCmdID(),
		Kind:      CmdKindRunCancel,
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		SenderID:  "c:01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Msg:       &CancelCmd{WFID: WFID("wf-123"), Reason: "test"},
	}

	data, err := msgpack.Marshal(&cmd)
	require.NoError(t, err)

	var decoded Command
	require.NoError(t, msgpack.Unmarshal(data, &decoded))

	require.Equal(t, cmd.SenderID, decoded.SenderID)
}

func TestCommand_EmptySenderID_EncodeError(t *testing.T) {
	cmd := Command{
		ID:        NewCmdID(),
		Kind:      CmdKindRunCancel,
		Timestamp: time.Now().UTC(),
		SenderID:  "", // deliberately empty
		Msg:       &CancelCmd{WFID: WFID("wf-123")},
	}

	_, err := msgpack.Marshal(&cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sender ID cannot be empty")
}
