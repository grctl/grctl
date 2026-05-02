package machine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"grctl/server/config"
	"grctl/server/store"
	ext "grctl/server/types/external/v1"
)

var ErrRunTerminal = errors.New("workflow run is in a terminal state")

type RunStore interface {
	CreateRunInfo(ctx context.Context, info *ext.RunInfo) error
	GetRunByWFID(ctx context.Context, wfID ext.WFID) (ext.RunInfo, uint64, error)
	PublishDirective(ctx context.Context, d ext.Directive) error
}

type RunAPI struct {
	store  RunStore
	config *config.DefaultsConfig
}

func NewRunAPI(
	store RunStore,
	config *config.DefaultsConfig,
) *RunAPI {
	return &RunAPI{
		store:  store,
		config: config,
	}
}

func (m *RunAPI) StartRun(ctx context.Context, cmd *ext.StartCmd) error {
	timeout := uint32(m.config.StepTimeout.Milliseconds())
	directive := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Kind:      ext.DirectiveKindStart,
		Timestamp: time.Now().UTC(),
		RunInfo:   cmd.RunInfo,
		Msg: &ext.Start{
			Input:   cmd.Input,
			Timeout: timeout,
		},
	}

	// Create workflow run record (also checks if already running)
	if err := m.store.CreateRunInfo(ctx, &cmd.RunInfo); err != nil {
		if errors.Is(err, store.ErrWorkflowAlreadyRunning) {
			return err
		}
		return fmt.Errorf("failed to create run in store: %w", err)
	}

	slog.Debug("enqueuing start directive", "runID", cmd.RunInfo.ID)
	if err := m.store.PublishDirective(ctx, directive); err != nil {
		return fmt.Errorf("failed to enqueue start directive: %w", err)
	}

	slog.Debug("workflow started", "workflowID", cmd.RunInfo.WFID, "runID", cmd.RunInfo.ID)
	return nil
}

// Send sends an event to a running workflow.
// The event will be delivered to the inbox stream.
// and then to the workflow only if the workflow in WaitEevent state.
// Otherwise, the event will be queued until the workflow transitions to WaitEvent state.
func (m *RunAPI) Send(ctx context.Context, cmd *ext.Command) error {
	msg, ok := cmd.Msg.(*ext.EventCmd)
	if !ok || msg == nil {
		return fmt.Errorf("expected EventCmd message but got %T", cmd.Msg)
	}

	eventName := msg.EventName
	payload := msg.Payload
	wfID := msg.WFID
	runInfo, _, err := m.store.GetRunByWFID(ctx, wfID)
	if err != nil {
		return fmt.Errorf("failed to get run info from store: %w", err)
	}
	if runInfo.Status.IsTerminal() {
		return ErrRunTerminal
	}

	directive := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Kind:      ext.DirectiveKindEvent,
		Timestamp: time.Now().UTC(),
		RunInfo:   runInfo,
		Msg: &ext.Event{
			EventName: eventName,
			Payload:   payload,
		},
	}

	if err := m.store.PublishDirective(ctx, directive); err != nil {
		return fmt.Errorf("failed to enqueue event directive: %w", err)
	}

	slog.Debug("event sent to workflow", "workflowID", wfID, "eventName", eventName, "runInfo", runInfo)
	return nil
}

func (m *RunAPI) DescribeRun(ctx context.Context, wfID ext.WFID) (ext.RunInfo, error) {
	runInfo, _, err := m.store.GetRunByWFID(ctx, wfID)
	if err != nil {
		return ext.RunInfo{}, fmt.Errorf("failed to get run info: %w", err)
	}

	return runInfo, nil
}

func (m *RunAPI) Cancel(ctx context.Context, cmd *ext.Command) error {
	msg, ok := cmd.Msg.(*ext.CancelCmd)
	if !ok || msg == nil {
		return fmt.Errorf("expected CancelCmd message but got %T", cmd.Msg)
	}

	reason := msg.Reason
	wfID := msg.WFID
	runInfo, _, err := m.store.GetRunByWFID(ctx, wfID)
	if err != nil {
		return fmt.Errorf("failed to get run info from store: %w", err)
	}
	if runInfo.Status.IsTerminal() {
		return ErrRunTerminal
	}

	directive := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Kind:      ext.DirectiveKindCancel,
		Timestamp: time.Now().UTC(),
		RunInfo:   runInfo,
		Msg: &ext.Cancel{
			Reason: reason,
		},
	}

	if err := m.store.PublishDirective(ctx, directive); err != nil {
		return fmt.Errorf("failed to enqueue cancel directive: %w", err)
	}

	slog.Debug("cancel directive enqueued", "workflowID", wfID, "reason", reason)
	return nil
}
