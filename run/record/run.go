package record

import (
	"fmt"
	"grctl/server/run/history"
	model "grctl/server/types"
	ext "grctl/server/types/external/v1"
	"log/slog"
	"time"
)

// parentCallbackRecords returns the background-task record that delivers this run's
// terminal outcome to its parent's callback step, or nil if the run was not started
// with a completion callback. result is set for completed outcomes; errDetails is set
// for failed/cancelled outcomes so the parent can branch on a single callback.
func parentCallbackRecords(d ext.Directive, status ext.RunStatus, result any, errDetails *ext.ErrorDetails) ([]model.Record, error) {
	ri := d.RunInfo
	if ri.ParentWFID == nil || ri.ParentCallbackStep == nil {
		return nil, nil
	}

	task, err := ext.NewNotifyParentCompleteTask(d.ID, ext.NotifyParentCompletePayload{
		ParentWFID: *ri.ParentWFID,
		StepName:   *ri.ParentCallbackStep,
		ChildWFID:  ri.WFID,
		Status:     status,
		Result:     result,
		Error:      errDetails,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build notify parent complete task: %w", err)
	}

	return []model.Record{model.BackgroundTaskRecord{Task: task}}, nil
}

func Start(d ext.Directive) ([]model.Record, error) {
	records := make([]model.Record, 0, 4)
	ri := d.RunInfo

	h, err := history.RunStarted(d, *ri.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create run started history event: %w", err)
	}
	historyAppend := model.HistoryRecord{History: h}
	runInfoUpdate := model.RunInfoRecord{Info: ri}

	records = append(records, historyAppend)
	records = append(records, runInfoUpdate)

	if start, ok := d.Msg.(*ext.Start); ok && start.Input != nil {
		records = append(records, model.RunInputRecord{
			WFID:  d.RunInfo.WFID,
			RunID: d.RunInfo.ID,
			Input: start.Input,
		})
	}

	slog.Debug("StartRun transition created")

	return records, nil
}

func CompleteRun(d ext.Directive, currentState ext.RunState) ([]model.Record, error) {
	records := make([]model.Record, 0, 5)
	ri := d.RunInfo
	ri, err := ri.Complete(d.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to create run completed state update: %w", err)
	}
	records = append(records, model.RunInfoRecord{Info: ri})

	h, err := history.RunCompleted(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run completed history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: h})

	state := currentState
	state.Kind = ext.RunStateComplete
	state.EnteredAt = time.Now().UTC()
	state.ActiveDirectiveID = ""

	records = append(records, model.RunStateRecord{
		State:       state,
		ExpectedSeq: currentState.SeqID,
	})

	var result any
	if msg, ok := d.Msg.(*ext.Complete); ok && msg.Result != nil {
		result = msg.Result
		records = append(records, model.RunOutputRecord{
			WFID:   d.RunInfo.WFID,
			RunID:  d.RunInfo.ID,
			Result: msg.Result,
		})
	}

	callbackRecords, err := parentCallbackRecords(d, ext.RunStatusCompleted, result, nil)
	if err != nil {
		return nil, err
	}
	records = append(records, callbackRecords...)

	purgeTask, err := ext.NewPurgeRunResidueTask(d.ID, d.RunInfo.WFID)
	if err != nil {
		return nil, fmt.Errorf("build purge run residue task: %w", err)
	}
	records = append(records, model.BackgroundTaskRecord{Task: purgeTask})

	return records, nil
}

func TerminateRun(d ext.Directive, currentState ext.RunState) ([]model.Record, error) {
	records := make([]model.Record, 0, 6)

	ri := d.RunInfo
	ri, err := ri.Terminate(d.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to create run terminated state update: %w", err)
	}
	records = append(records, model.RunInfoRecord{Info: ri})

	h, err := history.RunTerminated(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run terminated history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: h})

	state := currentState
	state.Kind = ext.RunStateTerminate
	state.EnteredAt = time.Now().UTC()
	state.ActiveDirectiveID = ""

	records = append(records, model.RunStateRecord{
		State:       state,
		ExpectedSeq: currentState.SeqID,
	})

	if currentState.WorkerID != nil {
		terminateTask, err := ext.NewWorkerTerminateRunTask(d.ID, *currentState.WorkerID, currentState.RunID)
		if err != nil {
			return nil, fmt.Errorf("failed to create worker terminate run task: %w", err)
		}
		records = append(records, model.BackgroundTaskRecord{Task: terminateTask})
	}

	termErr := &ext.ErrorDetails{Type: "Terminated", Message: "run was terminated"}
	if msg, ok := d.Msg.(*ext.Terminate); ok && msg.Reason != "" {
		termErr.Message = msg.Reason
	}
	callbackRecords, err := parentCallbackRecords(d, ext.RunStatusTerminated, nil, termErr)
	if err != nil {
		return nil, err
	}
	records = append(records, callbackRecords...)

	purgeTask, err := ext.NewPurgeRunResidueTask(d.ID, d.RunInfo.WFID)
	if err != nil {
		return nil, fmt.Errorf("build purge run residue task: %w", err)
	}
	records = append(records, model.BackgroundTaskRecord{Task: purgeTask})

	return records, nil
}

func CancelReceived(d ext.Directive) ([]model.Record, error) {
	records := make([]model.Record, 0, 2)
	records = append(records, model.InboxRecord{Directive: d})

	he, err := history.RunCancelScheduled(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run cancel scheduled history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: he})

	return records, nil
}

func CancelRun(d ext.Directive, currentState ext.RunState) ([]model.Record, error) {
	records := make([]model.Record, 0, 4)
	ri := d.RunInfo
	ri, err := ri.Cancel(d.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to create run cancelled state update: %w", err)
	}
	records = append(records, model.RunInfoRecord{Info: ri})

	h, err := history.RunCancelled(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run cancelled history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: h})

	state := currentState
	state.Kind = ext.RunStateCancel
	state.EnteredAt = time.Now().UTC()
	state.ActiveDirectiveID = ""

	records = append(records, model.RunStateRecord{
		State:       state,
		ExpectedSeq: currentState.SeqID,
	})

	cancelErr := &ext.ErrorDetails{Type: "Cancelled", Message: "child workflow was cancelled"}
	if msg, ok := d.Msg.(*ext.Cancel); ok && msg.Reason != "" {
		cancelErr.Message = msg.Reason
	}
	callbackRecords, err := parentCallbackRecords(d, ext.RunStatusCancelled, nil, cancelErr)
	if err != nil {
		return nil, err
	}
	records = append(records, callbackRecords...)

	purgeTask, err := ext.NewPurgeRunResidueTask(d.ID, d.RunInfo.WFID)
	if err != nil {
		return nil, fmt.Errorf("build purge run residue task: %w", err)
	}
	records = append(records, model.BackgroundTaskRecord{Task: purgeTask})

	return records, nil
}

func FailRun(d ext.Directive, currentState ext.RunState) ([]model.Record, error) {
	records := make([]model.Record, 0, 5)
	msg, ok := d.Msg.(*ext.Fail)
	if !ok || msg == nil {
		return nil, fmt.Errorf("can not create run failed state update: expected RunFailed but got %T", d.Msg)
	}

	ri := d.RunInfo
	ri, err := ri.Fail(d.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to create run failed state update: %w", err)
	}
	records = append(records, model.RunInfoRecord{Info: ri})

	he, err := history.RunFailed(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run failed history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: he})

	state := currentState
	state.Kind = ext.RunStateFail
	state.EnteredAt = time.Now().UTC()
	state.ActiveDirectiveID = ""

	records = append(records, model.RunStateRecord{
		State:       state,
		ExpectedSeq: currentState.SeqID,
	})

	records = append(records, model.RunErrorRecord{
		WFID:  d.RunInfo.WFID,
		RunID: d.RunInfo.ID,
		Error: msg.Error,
	})

	if currentState.WorkerID != nil {
		terminateTask, err := ext.NewWorkerTerminateRunTask(d.ID, *currentState.WorkerID, currentState.RunID)
		if err != nil {
			return nil, fmt.Errorf("failed to create worker terminate run task: %w", err)
		}
		records = append(records, model.BackgroundTaskRecord{Task: terminateTask})
	}

	failErr := msg.Error
	callbackRecords, err := parentCallbackRecords(d, ext.RunStatusFailed, nil, &failErr)
	if err != nil {
		return nil, err
	}
	records = append(records, callbackRecords...)

	purgeTask, err := ext.NewPurgeRunResidueTask(d.ID, d.RunInfo.WFID)
	if err != nil {
		return nil, fmt.Errorf("build purge run residue task: %w", err)
	}
	records = append(records, model.BackgroundTaskRecord{Task: purgeTask})

	return records, nil
}

func BuildUnexpectedFailure(d ext.Directive, cause string, currentState ext.RunState) ([]model.Record, error) {
	failDirective := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindFail,
		RunInfo:   d.RunInfo,
		Msg: &ext.Fail{
			Error: ext.ErrorDetails{
				Type:    "UnexpectedFailure",
				Message: fmt.Sprintf("An unexpected error occurred while processing directive %s of kind %s: %s", d.ID, d.Kind, cause),
			},
		},
	}

	return FailRun(failDirective, currentState)
}
