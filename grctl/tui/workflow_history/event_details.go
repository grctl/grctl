package workflow_history

import (
	"encoding/json"
	"fmt"

	ext "grctl/server/types/external/v1"
)

// key styles a label with muted foreground (256-color 244) without resetting
// other attributes like background, so it works inside styled table rows.
func key(label string) string {
	return "\033[38;5;244m" + label + "\033[39m"
}

// eventDetails returns a short human-readable summary of the event payload.
func eventDetails(event *ext.HistoryEvent) string {
	switch msg := event.Msg.(type) {
	case *ext.StepStarted:
		return stepDetail(msg.StepName)
	case *ext.StepCompleted:
		return stepCompletedDetail(msg)
	case *ext.StepFailed:
		return stepFailedDetail(msg)
	case *ext.StepCancelled:
		return stepDetail(msg.StepName)
	case *ext.StepTimedout:
		return stepDetail(msg.StepName)
	case *ext.RunScheduled:
		return runIDDetail(event.RunID)
	case *ext.RunStarted:
		return runStartedDetail(event.RunID, msg)
	case *ext.RunCompleted:
		return runCompletedDetail(event.RunID, msg)
	case *ext.RunFailed:
		return runFailedDetail(msg)
	case *ext.RunCancelScheduled:
		return runIDDetail(event.RunID)
	case *ext.RunCancelled:
		return runCancelledDetail(event.RunID, msg)
	case *ext.RunTimeout:
		return runIDDetail(event.RunID)
	case *ext.EventReceived:
		return eventReceivedDetail(msg)
	case *ext.TaskStarted:
		return taskStartedDetail(msg)
	case *ext.TaskCompleted:
		return taskCompletedDetail(msg)
	case *ext.TaskFailed:
		return taskFailedDetail(msg)
	case *ext.TaskCancelled:
		return taskDetail(msg.TaskName)
	case *ext.ChildWorkflowStarted:
		return childStartedDetail(msg)
	case *ext.ParentEventSent:
		return parentEventSentDetail(msg)
	}
	return "-"
}

func runIDDetail(runID ext.RunID) string {
	return key("Run ID:") + " " + runID.String()
}

func runStartedDetail(runID ext.RunID, msg *ext.RunStarted) string {
	return key("Run ID:") + " " + runID.String() + " " + key("Input:") + " " + formatJSON(msg.Input)
}

func runCompletedDetail(runID ext.RunID, msg *ext.RunCompleted) string {
	return key("Run ID:") + " " + runID.String() + " " + key("Result:") + " " + formatJSON(msg.Result)
}

func runFailedDetail(msg *ext.RunFailed) string {
	return errorDetail(msg.Error)
}

func runCancelledDetail(runID ext.RunID, msg *ext.RunCancelled) string {
	return key("Run ID:") + " " + runID.String() + " " + key("Reason:") + " " + msg.Reason
}

func stepDetail(name string) string {
	return key("Step:") + " " + name
}

func stepCompletedDetail(msg *ext.StepCompleted) string {
	detail := key("Step:") + " " + msg.StepName
	if msg.WorkerID != "" {
		detail += " " + key("Worker:") + " " + string(msg.WorkerID)
	}
	return detail
}

func stepFailedDetail(msg *ext.StepFailed) string {
	return key("Step:") + " " + msg.StepName + " " + errorDetail(msg.Error)
}

func eventReceivedDetail(msg *ext.EventReceived) string {
	return key("Event:") + " " + msg.EventName + " " + key("Payload:") + " " + formatJSON(msg.Payload)
}

func taskDetail(name string) string {
	return key("Task:") + " " + name
}

func taskStartedDetail(msg *ext.TaskStarted) string {
	return key("Task:") + " " + msg.TaskName + " " + key("Input:") + " " + formatJSON(msg.Args)
}

func taskCompletedDetail(msg *ext.TaskCompleted) string {
	return key("Task:") + " " + msg.TaskName + " " + key("Output:") + " " + formatJSON(msg.Output)
}

func taskFailedDetail(msg *ext.TaskFailed) string {
	return key("Task:") + " " + msg.TaskName + " " + errorDetail(msg.Error)
}

func parentEventSentDetail(msg *ext.ParentEventSent) string {
	detail := key("Event:") + " " + msg.EventName
	if msg.ParentWFType != "" {
		detail += " " + key("Parent:") + " " + msg.ParentWFType
	}
	detail += " " + key("Payload:") + " " + formatJSON(msg.Payload)
	return detail
}

func childStartedDetail(msg *ext.ChildWorkflowStarted) string {
	return key("Child:") + " " + msg.WFType + " " + key("Input:") + " " + formatJSON(msg.Input)
}

func errorDetail(err ext.ErrorDetails) string {
	return key("Error:") + " " + err.Type + " " + err.Message
}

func formatJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
