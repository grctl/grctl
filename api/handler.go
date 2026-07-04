package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"grctl/server/metrics"
	"grctl/server/run"
	"grctl/server/types/external/v1"
)

var (
	ErrUnknownAPICommand  = errors.New("unknown API command")
	ErrInvalidMessageType = errors.New("message type does not match command kind")
)

type APIHandler struct {
	runSvc  *run.Service
	metrics metrics.Recorder
}

func NewAPIHandler(runSvc *run.Service, metricsRecorder metrics.Recorder) *APIHandler {
	return &APIHandler{runSvc: runSvc, metrics: metricsRecorder}
}

func (h *APIHandler) handleMessage(msg external.Command) (payload any, err error) {
	defer func() {
		h.metrics.RecordAPICommand(context.Background(), string(msg.Kind), err == nil)
	}()

	switch msg.Kind {
	case external.CmdKindRunStart:
		return nil, h.handleStart(msg)
	case external.CmdKindRunCancel:
		return nil, h.handleCancel(msg)
	case external.CmdKindRunDescribe:
		return h.handleDescribe(msg)
	case external.CmdKindRunTerminate:
		return nil, h.handleTerminate(msg)
	case external.CmdKindRunEvent:
		return nil, h.handleEvent(msg)
	case external.CmdKindWorkerRegister:
		return nil, h.handleRegister(msg)
	default:
		return nil, ErrUnknownAPICommand
	}
}

func (h *APIHandler) handleStart(cmd external.Command) error {
	start, ok := cmd.Msg.(*external.StartCmd)
	if !ok {
		return fmt.Errorf("%w: expected StartCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	ctx := context.Background()
	return h.runSvc.StartRun(ctx, *start)
}

func (h *APIHandler) handleCancel(cmd external.Command) error {
	_, ok := cmd.Msg.(*external.CancelCmd)
	if !ok {
		return fmt.Errorf("%w: expected CancelCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	ctx := context.Background()
	return h.runSvc.Cancel(ctx, cmd)
}

func (h *APIHandler) handleDescribe(cmd external.Command) (any, error) {
	describeCmd, ok := cmd.Msg.(*external.DescribeCmd)
	if !ok {
		return nil, fmt.Errorf("%w: expected DescribeCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	ctx := context.Background()
	return h.runSvc.DescribeRun(ctx, describeCmd.WFID)
}

func (h *APIHandler) handleTerminate(cmd external.Command) error {
	_, ok := cmd.Msg.(*external.TerminateCmd)
	if !ok {
		return fmt.Errorf("%w: expected TerminateCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	ctx := context.Background()
	return h.runSvc.Terminate(ctx, cmd)
}

func (h *APIHandler) handleEvent(cmd external.Command) error {
	_, ok := cmd.Msg.(*external.EventCmd)
	if !ok {
		return fmt.Errorf("%w: expected EventCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	ctx := context.Background()
	return h.runSvc.Send(ctx, cmd)
}

func (h *APIHandler) handleRegister(cmd external.Command) error {
	register, ok := cmd.Msg.(*external.RegisterCmd)
	if !ok {
		return fmt.Errorf("%w: expected RegisterCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	ctx := context.Background()
	if err := h.runSvc.Register(ctx, *register); err != nil {
		return err
	}

	slog.Debug("worker registered workflow types", "worker_id", register.WorkerID, "type_count", len(register.Types))
	return nil
}
