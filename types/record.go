package types

import ext "grctl/server/types/external/v1"

type Record interface{ IsRecord() }

type RunInfoRecord struct {
	Info        ext.RunInfo
	ExpectedSeq uint64 // If > 0, NATS rejects the publish if the subject's last sequence differs
}

func (u RunInfoRecord) IsRecord() {}

type RunInputRecord struct {
	WFID  ext.WFID
	RunID ext.RunID
	Input any
}

func (u RunInputRecord) IsRecord() {}

type RunOutputRecord struct {
	WFID   ext.WFID
	RunID  ext.RunID
	Result any
}

func (u RunOutputRecord) IsRecord() {}

type RunErrorRecord struct {
	WFID  ext.WFID
	RunID ext.RunID
	Error ext.ErrorDetails
}

func (u RunErrorRecord) IsRecord() {}

type RunStateRecord struct {
	State       ext.RunState
	ExpectedSeq uint64 // If > 0, NATS rejects publish if the subject's last sequence differs
}

func (u RunStateRecord) IsRecord() {}

type TimerRecord struct {
	Timer ext.Timer
}

func (c TimerRecord) IsRecord() {}

type HistoryRecord struct {
	History ext.HistoryEvent
}

func (h HistoryRecord) IsRecord() {}

type InboxRecord struct {
	Directive ext.Directive
}

func (i InboxRecord) IsRecord() {}

type KVRecord struct {
	WFID  ext.WFID
	RunID ext.RunID
	KVMap map[string]any
}

func (u KVRecord) IsRecord() {}

type WorkerTaskDispatch struct {
	Directive ext.Directive
}

func (u WorkerTaskDispatch) IsRecord() {}

type DirectiveRecord struct {
	Directive ext.Directive
}

func (u DirectiveRecord) IsRecord() {}

type BackgroundTaskRecord struct {
	Task ext.BackgroundTask
}

func (u BackgroundTaskRecord) IsRecord() {}
