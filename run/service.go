package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"grctl/server/config"
	"grctl/server/metrics"
	model "grctl/server/types"
	ext "grctl/server/types/external/v1"
)

type RunStore interface {
	CreateRunInfo(ctx context.Context, info ext.RunInfo) error
	GetRunByWFID(ctx context.Context, wfID ext.WFID) (ext.RunInfo, uint64, error)
	PublishDirective(ctx context.Context, d ext.Directive) error
}

// TypeRegistry is the interface Service uses to look up and store workflow type
// definitions. The zero value of StartStepTimeoutMS means "use server default".
type TypeRegistry interface {
	PutTypes(ctx context.Context, workerID string, defs []ext.WorkflowTypeDef) error
	GetStartStepTimeout(ctx context.Context, wfType ext.WFType) (uint32, error)
	GetEventDef(ctx context.Context, wfType ext.WFType, eventName string) (ext.EventDef, error)
}

type Service struct {
	store    RunStore
	config   *config.DefaultsConfig
	registry TypeRegistry
	metrics  metrics.Recorder
}

func NewService(store RunStore, config *config.DefaultsConfig, registry TypeRegistry, metricsRecorder metrics.Recorder) *Service {
	return &Service{
		store:    store,
		config:   config,
		registry: registry,
		metrics:  metricsRecorder,
	}
}

func (m *Service) Register(ctx context.Context, cmd ext.RegisterCmd) error {
	return m.registry.PutTypes(ctx, cmd.WorkerID, cmd.Types)
}

func (m *Service) StartRun(ctx context.Context, cmd ext.StartCmd) error {
	timeout, err := m.registry.GetStartStepTimeout(ctx, cmd.RunInfo.WFType)
	if err != nil {
		return err
	}
	if timeout == 0 {
		timeout = uint32(m.config.StepTimeout.Milliseconds())
	}

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
	if err := m.store.CreateRunInfo(ctx, cmd.RunInfo); err != nil {
		if errors.Is(err, model.ErrWorkflowAlreadyRunning) {
			return err
		}
		return fmt.Errorf("failed to create run in store: %w", err)
	}

	slog.Debug("enqueuing start directive", "run_id", cmd.RunInfo.ID)
	if err := m.store.PublishDirective(ctx, directive); err != nil {
		return fmt.Errorf("failed to enqueue start directive: %w", err)
	}

	m.metrics.RecordRunStarted(ctx, string(cmd.RunInfo.WFType))
	slog.Debug("workflow started", "workflow_id", cmd.RunInfo.WFID, "run_id", cmd.RunInfo.ID)
	return nil
}

// Send sends an event to a running workflow.
// The event will be delivered to the inbox stream.
// and then to the workflow only if the workflow in WaitEevent state.
// Otherwise, the event will be queued until the workflow transitions to WaitEvent state.
func (m *Service) Send(ctx context.Context, cmd ext.Command) error {
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
		return model.ErrRunTerminal
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

	eventDef, err := m.registry.GetEventDef(ctx, runInfo.WFType, eventName)
	if err == nil && eventDef.TimeoutMS > 0 {
		directive.Msg.(*ext.Event).Timeout = eventDef.TimeoutMS
	}

	if err := m.store.PublishDirective(ctx, directive); err != nil {
		return fmt.Errorf("failed to enqueue event directive: %w", err)
	}

	slog.Debug("event sent to workflow", "workflow_id", wfID, "event_name", eventName, "run_info", runInfo)
	return nil
}

func (m *Service) DescribeRun(ctx context.Context, wfID ext.WFID) (ext.RunInfo, error) {
	runInfo, _, err := m.store.GetRunByWFID(ctx, wfID)
	if err != nil {
		return ext.RunInfo{}, fmt.Errorf("failed to get run info: %w", err)
	}

	return runInfo, nil
}

func (m *Service) Terminate(ctx context.Context, cmd ext.Command) error {
	msg, ok := cmd.Msg.(*ext.TerminateCmd)
	if !ok || msg == nil {
		return fmt.Errorf("expected TerminateCmd message but got %T", cmd.Msg)
	}

	wfID := msg.WFID
	runInfo, _, err := m.store.GetRunByWFID(ctx, wfID)
	if err != nil {
		return fmt.Errorf("failed to get run info from store: %w", err)
	}
	if runInfo.Status.IsTerminal() {
		return model.ErrRunTerminal
	}

	directive := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Kind:      ext.DirectiveKindTerminate,
		Timestamp: time.Now().UTC(),
		RunInfo:   runInfo,
		Msg:       &ext.Terminate{Reason: msg.Reason},
	}

	if err := m.store.PublishDirective(ctx, directive); err != nil {
		return fmt.Errorf("failed to enqueue terminate directive: %w", err)
	}

	slog.Debug("terminate directive enqueued", "workflow_id", wfID, "reason", msg.Reason)
	return nil
}

func (m *Service) Cancel(ctx context.Context, cmd ext.Command) error {
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
		return model.ErrRunTerminal
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

	slog.Debug("cancel directive enqueued", "workflow_id", wfID, "reason", reason)
	return nil
}
