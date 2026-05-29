package run

import (
	"context"
	"fmt"
	"grctl/server/run/record"
	model "grctl/server/types"
	ext "grctl/server/types/external/v1"
	"log/slog"
	"time"
)

// Plan turns a directive into the records that should be committed for it.
//
// Plan is the decision layer between the directive consumer and the store:
// given the current run snapshot and an incoming directive, it decides what
// should happen (which state transitions, history events, timers, and
// background tasks the directive implies) and returns those as a batch of
// records for Manager to commit atomically.
//
// Plan is pure: it performs no I/O, mutates no state, and returns the same
// records for the same (directive, snapshot) input.
//
// Callers must ensure the run is non-terminal before calling Plan; Plan does
// not re-check terminal status
func plan(ctx context.Context, d ext.Directive, sn model.StateSnapshot) ([]model.Record, error) {
	switch d.Kind {
	case ext.DirectiveKindStart:
		return planFromRunStart(ctx, d)
	case ext.DirectiveKindStepResult:
		return planFromStepResult(ctx, d, sn)
	case ext.DirectiveKindEvent:
		return record.EventReceived(d, sn)
	case ext.DirectiveKindWakeFromInbox:
		return planWakeFromInbox(ctx, sn)
	default:
		return planTransition(ctx, d, sn)
	}
}

func planFromRunStart(_ context.Context, d ext.Directive) ([]model.Record, error) {
	_, ok := d.Msg.(*ext.Start)
	if !ok {
		return nil, fmt.Errorf("expected Start message but got %T", d.Msg)
	}

	records := make([]model.Record, 0, 4)
	startTime := time.Now().UTC()
	ri := d.RunInfo

	ri, err := ri.Start(startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to create run started state update: %w", err)
	}

	// Update the directive's RunInfo so the timer created in StartStep
	// carries the correct StartedAt (needed for timeout failure paths).
	d.RunInfo = ri

	runState := ext.NewRunState(d, ext.RunStateStart)
	runState.StartedAt = ri.StartedAt
	runStateUpdate := model.RunStateRecord{
		State: runState,
	}

	runStartRecords, err := record.Start(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run start records: %w", err)
	}

	records = append(records, runStartRecords...)
	records = append(records, runStateUpdate)

	// TODO: We should use CreateTransitionRecords here instead of StepStart
	startStepRecords, err := record.StepStart(d, runState)
	if err != nil {
		return nil, fmt.Errorf("failed to create start step records during run start: %w", err)
	}

	records = append(records, startStepRecords...)

	return records, nil
}

func planFromStepResult(ctx context.Context, d ext.Directive, sn model.StateSnapshot) ([]model.Record, error) {
	var records []model.Record
	msg, ok := d.Msg.(*ext.StepResult)
	if !ok {
		return nil, fmt.Errorf("expected StepResult message but got %T", d.Msg)
	}

	nextD := deriveNextDirective(d, msg)

	if nextD.Kind == ext.DirectiveKindFailStep {
		failStep, ok := nextD.Msg.(*ext.FailStep)
		if !ok {
			return nil, fmt.Errorf("expected FailStep next msg but got %T", nextD.Msg)
		}

		stepFailRecords, err := record.StepFail(d, sn.RunState)
		if err != nil {
			return nil, fmt.Errorf("failed to build step fail records: %w", err)
		}
		records = append(records, stepFailRecords...)

		// A failed step fails the run. FailRun expects a Fail directive
		// carrying the error, so derive one from the FailStep message.
		failDirective := ext.Directive{
			ID:        nextD.ID,
			Timestamp: nextD.Timestamp,
			Kind:      ext.DirectiveKindFail,
			RunInfo:   nextD.RunInfo,
			Msg:       &ext.Fail{Error: failStep.Error},
		}

		failRunRecords, err := record.FailRun(failDirective, sn.RunState)
		if err != nil {
			return nil, fmt.Errorf("failed to build fail run records: %w", err)
		}
		records = append(records, failRunRecords...)
		return records, nil
	}

	stepCompleteRecords, err := record.StepComplete(d, sn.RunState)
	if err != nil {
		return nil, fmt.Errorf("failed to build step complete records: %w", err)
	}
	records = append(records, stepCompleteRecords...)

	transitionRecords, err := planTransition(ctx, nextD, sn)
	if err != nil {
		return nil, fmt.Errorf("failed to build transition records: %w", err)
	}
	records = append(records, transitionRecords...)

	return records, nil
}

func planWakeFromInbox(ctx context.Context, sn model.StateSnapshot) ([]model.Record, error) {
	if sn.RunState.Kind != ext.RunStateWait || sn.Event.ID == "" {
		return nil, nil
	}

	return planTransition(ctx, sn.Event, sn)
}

// Non orchestrated directive transition records
func planTransition(_ context.Context, d ext.Directive, sn model.StateSnapshot) ([]model.Record, error) {
	currentState := sn.RunState
	slog.Debug(fmt.Sprintf("Creating records for directive %s", d.Kind))

	switch d.Kind {
	case ext.DirectiveKindStep:
		return record.StepStart(d, currentState)
	case ext.DirectiveKindEvent:
		return record.EventStart(d, currentState)
	case ext.DirectiveKindStepTimeout:
		return record.StepTimeout(d, currentState)
	case ext.DirectiveKindComplete:
		return record.CompleteRun(d, currentState)
	case ext.DirectiveKindFail:
		return record.FailRun(d, currentState)
	case ext.DirectiveKindCancel:
		return record.CancelRun(d, currentState)
	case ext.DirectiveKindWait:
		return record.Wait(d, sn)
	case ext.DirectiveKindWaitTimeout:
		return record.WaitTimeout(d, currentState)
	default:
		slog.Warn("unknown directive kind", "kind", d.Kind)
		// Unknown kinds should be ACKed to avoid retry loops
		return nil, fmt.Errorf("unknown directive kind: %s", d.Kind)
	}
}

func deriveNextDirective(d ext.Directive, msg *ext.StepResult) ext.Directive {
	return ext.Directive{
		ID:        ext.DeriveNextDirectiveID(d.ID),
		Timestamp: time.Now().UTC(),
		Kind:      msg.NextMsgKind,
		RunInfo:   d.RunInfo,
		Msg:       msg.NextMsg,
	}
}
