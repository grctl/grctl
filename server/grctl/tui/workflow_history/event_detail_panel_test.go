package workflow_history

import (
	"strings"
	"testing"
	"time"

	ext "grctl/server/types/external/v1"
)

const testWidth = 60

func testEvent(kind ext.HistoryKind, msg ext.HistoryMessage) *ext.HistoryEvent {
	return &ext.HistoryEvent{
		WFID:      "wf-test-id",
		RunID:     "run-test-id",
		WorkerID:  "worker-1",
		Timestamp: time.Date(2026, 3, 16, 9, 48, 21, 313000000, time.UTC),
		Kind:      kind,
		Msg:       msg,
	}
}

func TestBuildDetailContent_TaskCompleted(t *testing.T) {
	event := testEvent(ext.HistoryKindTaskCompleted, &ext.TaskCompleted{
		TaskID:     "task-abc-123",
		TaskName:   "incr",
		Output:     map[string]any{"c": float64(999)},
		StepName:   "incr_step",
		DurationMS: 5,
	})

	result := buildDetailContent(event, testWidth)

	assertContains(t, result, "TASK COMPLETED")
	assertContains(t, result, "09:48:21.313")
	assertContains(t, result, "incr")
	assertContains(t, result, "task-abc-123")
	assertContains(t, result, "incr_step")
	assertContains(t, result, "5ms")
	assertContains(t, result, "Output")
	assertContains(t, result, "999")
}

func TestBuildDetailContent_RunFailed(t *testing.T) {
	event := testEvent(ext.HistoryKindRunFailed, &ext.RunFailed{
		Error:      ext.ErrorDetails{Type: "RuntimeError", Message: "something broke"},
		DurationMS: 1500,
	})

	result := buildDetailContent(event, testWidth)

	assertContains(t, result, "RUN FAILED")
	assertContains(t, result, "1.50s")
	assertContains(t, result, "Error")
	assertContains(t, result, "RuntimeError")
	assertContains(t, result, "something broke")
}

func TestBuildDetailContent_StepStarted(t *testing.T) {
	event := testEvent(ext.HistoryKindStepStarted, &ext.StepStarted{
		StepName: "process_data",
	})

	result := buildDetailContent(event, testWidth)

	assertContains(t, result, "STEP STARTED")
	assertContains(t, result, "process_data")
	assertNotContains(t, result, "── Output")
	assertNotContains(t, result, "── Input")
	assertNotContains(t, result, "── Error")
}

func TestBuildDetailContent_RunStarted(t *testing.T) {
	event := testEvent(ext.HistoryKindRunStarted, &ext.RunStarted{
		Input: map[string]any{"user": "alice", "age": float64(30)},
	})

	result := buildDetailContent(event, testWidth)

	assertContains(t, result, "RUN STARTED")
	assertContains(t, result, "Input")
	assertContains(t, result, "alice")
	assertContains(t, result, "30")
}

func TestBuildDetailContent_NilMsg(t *testing.T) {
	event := testEvent(ext.HistoryKindRunStarted, nil)

	result := buildDetailContent(event, testWidth)

	assertContains(t, result, "RUN STARTED")
	assertContains(t, result, "09:48:21.313")
}

func TestBuildDetailContent_AllEventTypes(t *testing.T) {
	events := []*ext.HistoryEvent{
		testEvent(ext.HistoryKindRunScheduled, &ext.RunScheduled{}),
		testEvent(ext.HistoryKindRunStarted, &ext.RunStarted{}),
		testEvent(ext.HistoryKindRunCompleted, &ext.RunCompleted{Result: "ok", DurationMS: 100}),
		testEvent(ext.HistoryKindRunFailed, &ext.RunFailed{Error: ext.ErrorDetails{Type: "E", Message: "m"}, DurationMS: 50}),
		testEvent(ext.HistoryKindRunCancelScheduled, &ext.RunCancelScheduled{}),
		testEvent(ext.HistoryKindRunCancelReceived, &ext.RunCancelReceived{}),
		testEvent(ext.HistoryKindRunCancelled, &ext.RunCancelled{Reason: "user", DurationMS: 10}),
		testEvent(ext.HistoryKindRunTimeout, &ext.RunTimeout{DurationMS: 5000}),
		testEvent(ext.HistoryKindStepStarted, &ext.StepStarted{StepName: "s"}),
		testEvent(ext.HistoryKindStepCompleted, &ext.StepCompleted{StepName: "s", DurationMS: 10}),
		testEvent(ext.HistoryKindStepFailed, &ext.StepFailed{StepName: "s", Error: ext.ErrorDetails{Type: "E", Message: "m"}, DurationMS: 10}),
		testEvent(ext.HistoryKindStepCancelled, &ext.StepCancelled{StepName: "s"}),
		testEvent(ext.HistoryKindStepTimeout, &ext.StepTimedout{StepName: "s", DurationMS: 10}),
		testEvent(ext.HistoryKindWaitEventStarted, &ext.WaitEventStarted{}),
		testEvent(ext.HistoryKindEventReceived, &ext.EventReceived{EventName: "ev", Payload: "data"}),
		testEvent(ext.HistoryKindTaskStarted, &ext.TaskStarted{TaskID: "t1", TaskName: "tn", Args: "in", StepName: "s"}),
		testEvent(ext.HistoryKindTaskCompleted, &ext.TaskCompleted{TaskID: "t1", TaskName: "tn", Output: "out", StepName: "s", DurationMS: 10}),
		testEvent(ext.HistoryKindTaskFailed, &ext.TaskFailed{TaskID: "t1", TaskName: "tn", StepName: "s", Error: ext.ErrorDetails{Type: "E", Message: "m"}, DurationMS: 10}),
		testEvent(ext.HistoryKindTaskCancelled, &ext.TaskCancelled{TaskID: "t1", TaskName: "tn", StepName: "s", DurationMS: 10}),
	}

	for _, event := range events {
		t.Run(string(event.Kind), func(t *testing.T) {
			result := buildDetailContent(event, testWidth)
			if result == "" {
				t.Errorf("buildDetailContent returned empty string for %s", event.Kind)
			}
		})
	}
}

func TestBuildDetailContent_NilEvent(t *testing.T) {
	result := buildDetailContent(nil, testWidth)
	if result != "" {
		t.Errorf("expected empty string for nil event, got: %s", result)
	}
}

func TestBuildDetailContent_RunFailed_WithStackTrace(t *testing.T) {
	event := testEvent(ext.HistoryKindRunFailed, &ext.RunFailed{
		Error: ext.ErrorDetails{
			Type:    "ValueError",
			Message: "bad input",
			StackTrace: "Traceback (most recent call last):\n" +
				"  File \"workflow.py\", line 42, in run\n" +
				"    result = process(data)\n" +
				"ValueError: bad input\n",
		},
		DurationMS: 200,
	})

	result := buildDetailContent(event, testWidth)

	assertContains(t, result, "Stack Trace")
	assertContains(t, result, "Traceback (most recent call last):")
	assertContains(t, result, "workflow.py")
	assertContains(t, result, "ValueError: bad input")
}

func TestBuildDetailContent_ErrorWithoutStackTrace(t *testing.T) {
	event := testEvent(ext.HistoryKindTaskFailed, &ext.TaskFailed{
		TaskID:   "t1",
		TaskName: "compute",
		StepName: "step1",
		Error:    ext.ErrorDetails{Type: "RuntimeError", Message: "oops"},
	})

	result := buildDetailContent(event, testWidth)

	assertContains(t, result, "Error")
	assertContains(t, result, "RuntimeError")
	assertNotContains(t, result, "Stack Trace")
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected output to contain %q, got:\n%s", substr, s)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("expected output to NOT contain %q, got:\n%s", substr, s)
	}
}
