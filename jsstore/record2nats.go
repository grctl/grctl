package jsstore

import (
	"fmt"
	"grctl/server/natsreg"
	models "grctl/server/types"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

func recordToNats(record models.Record) ([]nats.Msg, error) {
	switch record := record.(type) {
	case models.RunInfoRecord:
		return RunInfoRecordToNatsMsgs(record)
	case models.RunInputRecord:
		return RunInputRecordToNatsMsgs(record)
	case models.RunOutputRecord:
		return RunOutputRecordToNatsMsgs(record)
	case models.RunErrorRecord:
		return RunErrorRecordToNatsMsgs(record)
	case models.RunStateRecord:
		return RunStateRecordToNatsMsgs(record)
	case models.TimerRecord:
		return TimerRecordToNatsMsgs(record)
	case models.HistoryRecord:
		return HistoryRecordToNatsMsgs(record)
	case models.InboxRecord:
		return InboxRecordToNatsMsgs(record)
	case models.KVRecord:
		return KVRecordToNatsMsgs(record)
	case models.WorkerTaskDispatch:
		return WorkerTaskDispatchToNatsMsgs(record)
	case models.DirectiveRecord:
		return DirectiveRecordToNatsMsgs(record)
	case models.BackgroundTaskRecord:
		return BackgroundTaskRecordToNatsMsgs(record)
	default:
		return nil, fmt.Errorf("unknown record type: %T", record)
	}
}

func RunInfoRecordToNatsMsgs(r models.RunInfoRecord) ([]nats.Msg, error) {
	i := r.Info
	msg := nats.Msg{}
	data, err := msgpack.Marshal(r.Info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal workflow run info: %w", err)
	}

	key := natsreg.Manifest.RunInfoKey(i.WFType, i.WFID, i.ID)
	msg.Subject = key
	msg.Data = data

	if r.ExpectedSeq > 0 {
		msg.Header = nats.Header{}
		msg.Header.Set("Nats-Expected-Last-Subject-Sequence", fmt.Sprintf("%d", r.ExpectedSeq))
	}

	return []nats.Msg{msg}, nil
}

func RunInputRecordToNatsMsgs(r models.RunInputRecord) ([]nats.Msg, error) {
	data, err := msgpack.Marshal(r.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run input: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.RunInputKey(r.WFID, r.RunID),
		Data:    data,
	}

	return []nats.Msg{msg}, nil
}

func RunOutputRecordToNatsMsgs(r models.RunOutputRecord) ([]nats.Msg, error) {
	data, err := msgpack.Marshal(r.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run output: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.RunOutputKey(r.WFID, r.RunID),
		Data:    data,
	}

	return []nats.Msg{msg}, nil
}

func RunErrorRecordToNatsMsgs(r models.RunErrorRecord) ([]nats.Msg, error) {
	data, err := msgpack.Marshal(r.Error)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run error: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.RunErrorKey(r.WFID, r.RunID),
		Data:    data,
	}

	return []nats.Msg{msg}, nil
}

func RunStateRecordToNatsMsgs(r models.RunStateRecord) ([]nats.Msg, error) {
	if r.State.WFID == "" || r.State.RunID == "" {
		return nil, fmt.Errorf("run state WFID and RunID are required")
	}

	data, err := msgpack.Marshal(r.State)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run state: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.RunStateSubject(r.State.WFID, r.State.RunID),
		Data:    data,
	}

	if r.ExpectedSeq > 0 {
		msg.Header = nats.Header{}
		msg.Header.Set("Nats-Expected-Last-Subject-Sequence", fmt.Sprintf("%d", r.ExpectedSeq))
	}

	return []nats.Msg{msg}, nil
}

func TimerRecordToNatsMsgs(r models.TimerRecord) ([]nats.Msg, error) {
	t := r.Timer
	data, err := msgpack.Marshal(&t)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal timer: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.TimerSubject(t.WFID, t.Kind, t.ID),
		Data:    data,
		Header:  nats.Header{},
	}
	msg.Header.Set("Nats-Schedule", fmt.Sprintf("@at %s", t.ExpiresAt.Format(time.RFC3339Nano)))
	msg.Header.Set("Nats-Schedule-Target", natsreg.Manifest.TimerFiredSubject())

	return []nats.Msg{msg}, nil
}

func HistoryRecordToNatsMsgs(r models.HistoryRecord) ([]nats.Msg, error) {
	h := r.History
	data, err := msgpack.Marshal(&h)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal history event: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.HistorySubject(h.WFID, h.RunID),
		Data:    data,
	}

	return []nats.Msg{msg}, nil
}

func InboxRecordToNatsMsgs(r models.InboxRecord) ([]nats.Msg, error) {
	d := r.Directive
	data, err := msgpack.Marshal(&d)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal directive: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.EventInboxSubject(r.Directive.RunInfo.WFID),
		Data:    data,
	}

	return []nats.Msg{msg}, nil
}

func KVRecordToNatsMsgs(r models.KVRecord) ([]nats.Msg, error) {
	msgs := make([]nats.Msg, 0, len(r.KVMap))

	for key, value := range r.KVMap {
		data, err := msgpack.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal KV update value for key %s: %w", key, err)
		}

		msgs = append(msgs, nats.Msg{
			Subject: natsreg.Manifest.WfKVKey(r.WFID, r.RunID, key),
			Data:    data,
		})
	}

	return msgs, nil
}

func WorkerTaskDispatchToNatsMsgs(r models.WorkerTaskDispatch) ([]nats.Msg, error) {
	d := r.Directive
	data, err := msgpack.Marshal(&d)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker task dispatch directive: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.WorkerTaskSubject(d.RunInfo.WFType, d.RunInfo.WFID, d.RunInfo.ID),
		Data:    data,
	}

	return []nats.Msg{msg}, nil
}

func DirectiveRecordToNatsMsgs(r models.DirectiveRecord) ([]nats.Msg, error) {
	d := r.Directive
	data, err := msgpack.Marshal(&d)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal directive record: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.DirectiveSubject(d.RunInfo.WFType, d.RunInfo.WFID, d.RunInfo.ID),
		Data:    data,
		Header:  nats.Header{},
	}
	msg.Header.Set(nats.MsgIdHdr, string(d.ID))

	return []nats.Msg{msg}, nil
}

func BackgroundTaskRecordToNatsMsgs(r models.BackgroundTaskRecord) ([]nats.Msg, error) {
	data, err := msgpack.Marshal(r.Task)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal background task: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.BgTaskSubject(),
		Data:    data,
		Header:  nats.Header{},
	}
	msg.Header.Set(nats.MsgIdHdr, string(r.Task.DeduplicationID))

	return []nats.Msg{msg}, nil
}
