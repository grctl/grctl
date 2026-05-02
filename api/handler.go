package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"grctl/server/machine"
	"grctl/server/types/external/v1"
)

var (
	ErrUnknownAPICommand  = errors.New("unknown API command")
	ErrInvalidMessageType = errors.New("message type does not match command kind")
)

type APIHandler struct {
	runAPI *machine.RunAPI
}

func NewAPIHandler(runAPI *machine.RunAPI) *APIHandler {
	return &APIHandler{runAPI: runAPI}
}

func (h *APIHandler) handleMessage(msg external.Command) (any, error) {
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
	return h.runAPI.StartRun(ctx, start)
}

func (h *APIHandler) handleCancel(cmd external.Command) error {
	_, ok := cmd.Msg.(*external.CancelCmd)
	if !ok {
		return fmt.Errorf("%w: expected CancelCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	ctx := context.Background()
	return h.runAPI.Cancel(ctx, &cmd)
}

func (h *APIHandler) handleDescribe(cmd external.Command) (any, error) {
	describeCmd, ok := cmd.Msg.(*external.DescribeCmd)
	if !ok {
		return nil, fmt.Errorf("%w: expected DescribeCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	ctx := context.Background()
	return h.runAPI.DescribeRun(ctx, describeCmd.WFID)
}

func (h *APIHandler) handleTerminate(cmd external.Command) error {
	terminate, ok := cmd.Msg.(*external.TerminateCmd)
	if !ok {
		return fmt.Errorf("%w: expected TerminateCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	slog.Debug("Workflow terminated", "WorkflowID", terminate.WFID)
	return nil
}

func (h *APIHandler) handleEvent(cmd external.Command) error {
	_, ok := cmd.Msg.(*external.EventCmd)
	if !ok {
		return fmt.Errorf("%w: expected EventCmd for kind %s, got %T",
			ErrInvalidMessageType, cmd.Kind, cmd.Msg)
	}

	ctx := context.Background()
	return h.runAPI.Send(ctx, &cmd)
}
