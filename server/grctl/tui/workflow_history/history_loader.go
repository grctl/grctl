package workflow_history

import (
	"context"
	"fmt"
	"sort"
	"time"

	"grctl/server/api"
	"grctl/server/natsreg"
	"grctl/server/store"
	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/vmihailenco/msgpack/v5"
)

// HistoryLoader manages the NATS connection and fetches history events for a single run.
type HistoryLoader struct {
	nc       *nats.Conn
	store    *store.StateStore
	statusCh chan bool
}

// NewHistoryLoader connects to the server and returns a loader backed by StateStore.
func NewHistoryLoader(serverURL string) (*HistoryLoader, error) {
	statusCh := make(chan bool, 2)
	nc, err := nats.Connect(serverURL,
		nats.DisconnectErrHandler(func(_ *nats.Conn, _ error) {
			select {
			case statusCh <- false:
			default:
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			select {
			case statusCh <- true:
			default:
			}
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server at %s: %w", serverURL, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	stream, err := js.Stream(context.Background(), natsreg.Manifest.StateStreamName())
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to bind to state stream: %w", err)
	}

	stateStore := store.NewStateStore(js, stream)

	return &HistoryLoader{nc: nc, store: stateStore, statusCh: statusCh}, nil
}

// GetRunInfoByRunID resolves a run by run ID alone (no wfType or wfID needed).
func (l *HistoryLoader) GetRunInfoByRunID(ctx context.Context, runID ext.RunID) (ext.RunInfo, error) {
	return l.store.GetRunByRunID(ctx, runID)
}

// GetLatestRunInfo resolves the latest run for a workflow ID (no wfType needed).
func (l *HistoryLoader) GetLatestRunInfo(ctx context.Context, wfID ext.WFID) (ext.RunInfo, error) {
	info, _, err := l.store.GetRunByWFID(ctx, wfID)
	return info, err
}

// GetHistory fetches all history events for the given workflow run.
func (l *HistoryLoader) GetHistory(ctx context.Context, wfID ext.WFID, runID ext.RunID) ([]*ext.HistoryEvent, error) {
	events, err := l.store.GetHistoryForRun(ctx, wfID, runID)
	if err != nil {
		return nil, err
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})

	return events, nil
}

// GetRunDataFlags checks whether input, output, and error data exist for the given run.
func (l *HistoryLoader) GetRunDataFlags(ctx context.Context, wfID ext.WFID, runID ext.RunID) (hasInput, hasOutput, hasError bool, err error) {
	hasInput, err = l.store.HasRunInput(ctx, wfID, runID)
	if err != nil {
		return false, false, false, fmt.Errorf("failed to check run input: %w", err)
	}
	hasOutput, err = l.store.HasRunOutput(ctx, wfID, runID)
	if err != nil {
		return false, false, false, fmt.Errorf("failed to check run output: %w", err)
	}
	hasError, err = l.store.HasRunError(ctx, wfID, runID)
	if err != nil {
		return false, false, false, fmt.Errorf("failed to check run error: %w", err)
	}
	return hasInput, hasOutput, hasError, nil
}

// StatusCh returns a channel that receives connection status changes.
func (l *HistoryLoader) StatusCh() <-chan bool {
	return l.statusCh
}

// CancelWorkflow sends a cancel command for the given workflow ID.
func (l *HistoryLoader) CancelWorkflow(ctx context.Context, wfID ext.WFID) error {
	command := ext.Command{
		ID:        ext.NewCmdID(),
		Kind:      ext.CmdKindRunCancel,
		Timestamp: time.Now().UTC(),
		Msg: &ext.CancelCmd{
			WFID:   wfID,
			Reason: "cancelled via TUI",
		},
	}
	data, err := msgpack.Marshal(&command)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}
	subject := natsreg.Manifest.APISubject(wfID)
	msg, err := l.nc.RequestWithContext(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("cancel request failed: %w", err)
	}
	var resp api.GrctlAPIResponse
	if err := msgpack.Unmarshal(msg.Data, &resp); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error.Error())
	}
	return nil
}

// Close shuts down the NATS connection.
func (l *HistoryLoader) Close() {
	if l.nc != nil {
		l.nc.Close()
	}
}
