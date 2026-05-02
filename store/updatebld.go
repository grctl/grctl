package store

import (
	"fmt"
	"grctl/server/natsreg"
	"time"

	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

type StateUpdate interface {
	ToNatsMsgs() ([]*nats.Msg, error)
}

type RunInfoUpdate struct {
	Info        ext.RunInfo
	ExpectedSeq uint64 // If > 0, NATS rejects the publish if the subject's last sequence differs
}

func (u RunInfoUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	i := u.Info
	msg := nats.Msg{}
	data, err := msgpack.Marshal(u.Info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal workflow run info: %w", err)
	}

	key := natsreg.Manifest.RunInfoKey(i.WFType, i.WFID, i.ID)
	msg.Subject = key
	msg.Data = data

	if u.ExpectedSeq > 0 {
		msg.Header = nats.Header{}
		msg.Header.Set("Nats-Expected-Last-Subject-Sequence", fmt.Sprintf("%d", u.ExpectedSeq))
	}

	return []*nats.Msg{&msg}, nil
}

type RunInputUpdate struct {
	WFID  ext.WFID
	RunID ext.RunID
	Input any
}

func (u RunInputUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	data, err := msgpack.Marshal(u.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run input: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.RunInputKey(u.WFID, u.RunID),
		Data:    data,
	}

	return []*nats.Msg{&msg}, nil
}

type RunOutputUpdate struct {
	WFID   ext.WFID
	RunID  ext.RunID
	Result any
}

func (u RunOutputUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	data, err := msgpack.Marshal(u.Result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run output: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.RunOutputKey(u.WFID, u.RunID),
		Data:    data,
	}

	return []*nats.Msg{&msg}, nil
}

type RunErrorUpdate struct {
	WFID  ext.WFID
	RunID ext.RunID
	Error ext.ErrorDetails
}

func (u RunErrorUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	data, err := msgpack.Marshal(u.Error)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run error: %w", err)
	}

	msg := nats.Msg{
		Subject: natsreg.Manifest.RunErrorKey(u.WFID, u.RunID),
		Data:    data,
	}

	return []*nats.Msg{&msg}, nil
}

type RunStateUpdate struct {
	State       ext.RunState
	ExpectedSeq uint64 // If > 0, NATS rejects publish if the subject's last sequence differs
}

func (u RunStateUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	msg := nats.Msg{}
	data, err := msgpack.Marshal(u.State)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run state: %w", err)
	}

	if u.State.WFID == "" || u.State.RunID == "" {
		return nil, fmt.Errorf("run state WFID and RunID are required")
	}

	msg.Subject = natsreg.Manifest.RunStateSubject(u.State.WFID, u.State.RunID)
	msg.Data = data

	if u.ExpectedSeq > 0 {
		msg.Header = nats.Header{}
		msg.Header.Set("Nats-Expected-Last-Subject-Sequence", fmt.Sprintf("%d", u.ExpectedSeq))
	}

	return []*nats.Msg{&msg}, nil
}

type TimerUpdate struct {
	Timer ext.Timer
}

func (c TimerUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	t := c.Timer
	msg := nats.Msg{}
	data, err := msgpack.Marshal(&c.Timer)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal timer: %w", err)
	}

	msg.Subject = natsreg.Manifest.TimerSubject(t.WFID, t.Kind, t.ID)
	msg.Data = data
	msg.Header = nats.Header{}
	msg.Header.Set("Nats-Schedule", fmt.Sprintf("@at %s", t.ExpiresAt.Format(time.RFC3339Nano)))
	msg.Header.Set("Nats-Schedule-Target", natsreg.Manifest.TimerFiredSubject())

	return []*nats.Msg{&msg}, nil
}

type HistoryUpdate struct {
	History ext.HistoryEvent
}

func (a HistoryUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	h := a.History
	msg := nats.Msg{}
	data, err := msgpack.Marshal(&a.History)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal history event: %w", err)
	}

	msg.Subject = natsreg.Manifest.HistorySubject(h.WFID, h.RunID)
	msg.Data = data

	return []*nats.Msg{&msg}, nil
}

type InboxUpdate struct {
	Directive ext.Directive
}

func (u InboxUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	d := u.Directive
	msg := nats.Msg{}
	data, err := msgpack.Marshal(&u.Directive)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal directive: %w", err)
	}

	if d.Kind == ext.DirectiveKindCancel {
		// For cancel directives, publish to a wildcard subject that all runs of the workflow will subscribe to
		msg.Subject = natsreg.Manifest.CancelInboxSubject(d.RunInfo.WFID)
	} else {
		msg.Subject = natsreg.Manifest.EventInboxSubject(d.RunInfo.WFID)
	}

	msg.Data = data

	return []*nats.Msg{&msg}, nil
}

type KVUpdate struct {
	WFID    ext.WFID
	RunID   ext.RunID
	Updates map[string]any
}

func (u KVUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	msgs := make([]*nats.Msg, 0, len(u.Updates))

	for key, value := range u.Updates {
		msg := nats.Msg{}
		data, err := msgpack.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal KV update value for key %s: %w", key, err)
		}

		msg.Subject = natsreg.Manifest.WfKVKey(u.WFID, u.RunID, key)
		msg.Data = data

		msgs = append(msgs, &msg)
	}

	return msgs, nil
}

type WorkerTaskDispatch struct {
	Directive ext.Directive
}

func (u WorkerTaskDispatch) ToNatsMsgs() ([]*nats.Msg, error) {
	d := u.Directive
	msg := nats.Msg{}
	data, err := msgpack.Marshal(&u.Directive)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker task dispatch directive: %w", err)
	}

	msg.Subject = natsreg.Manifest.WorkerTaskSubject(d.RunInfo.WFType, d.RunInfo.WFID, d.RunInfo.ID)
	msg.Data = data

	return []*nats.Msg{&msg}, nil
}

type BackgroundTaskUpdate struct {
	Task ext.BackgroundTask
}

func (u BackgroundTaskUpdate) ToNatsMsgs() ([]*nats.Msg, error) {
	data, err := msgpack.Marshal(&u.Task)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal background task: %w", err)
	}

	msg := nats.Msg{}
	msg.Subject = natsreg.Manifest.BgTaskSubject()
	msg.Data = data
	msg.Header = nats.Header{}
	msg.Header.Set(nats.MsgIdHdr, string(u.Task.DeduplicationID))

	return []*nats.Msg{&msg}, nil
}
