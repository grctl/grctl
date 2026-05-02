package external

import (
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/vmihailenco/msgpack/v5"
)

type CmdKind string

type CmdID string

func NewCmdID() CmdID {
	return CmdID(ulid.Make().String())
}

const CmdKindRunCancel CmdKind = "run.cancel"
const CmdKindRunDescribe CmdKind = "run.describe"
const CmdKindRunEvent CmdKind = "run.event"
const CmdKindRunStart CmdKind = "run.start"
const CmdKindRunTerminate CmdKind = "run.terminate"

// Messages sent by the client
type StartCmd struct {
	RunInfo RunInfo `json:"run_info" msgpack:"run_info"`
	Input   *any    `json:"input" msgpack:"input"`
}

type CancelCmd struct {
	WFID   WFID   `json:"wf_id" msgpack:"wf_id"`
	Reason string `json:"reason" msgpack:"reason"`
}

type TerminateCmd struct {
	WFID   WFID   `json:"wf_id" msgpack:"wf_id"`
	Reason string `json:"reason" msgpack:"reason"`
}

type DescribeCmd struct {
	WFID WFID `json:"wf_id" msgpack:"wf_id"`
}

type EventCmd struct {
	WFID      WFID   `json:"wf_id" msgpack:"wf_id"`
	EventName string `json:"event_name" msgpack:"event_name"`
	Payload   *any   `json:"payload" msgpack:"payload"`
}

type Command struct {
	ID        CmdID          `json:"id" msgpack:"id"`
	Kind      CmdKind        `json:"kind" msgpack:"kind"`
	Timestamp time.Time      `json:"timestamp" msgpack:"timestamp"`
	Msg       CommandMessage `json:"msg" msgpack:"msg"`
}

// CommandMessage is an interface for command message types
type CommandMessage interface {
	isCommandMsg()
}

func (StartCmd) isCommandMsg()     {}
func (CancelCmd) isCommandMsg()    {}
func (DescribeCmd) isCommandMsg()  {}
func (TerminateCmd) isCommandMsg() {}
func (EventCmd) isCommandMsg()     {}

var commandMessageFactories = map[CmdKind]func() CommandMessage{
	CmdKindRunStart:     func() CommandMessage { return &StartCmd{} },
	CmdKindRunCancel:    func() CommandMessage { return &CancelCmd{} },
	CmdKindRunDescribe:  func() CommandMessage { return &DescribeCmd{} },
	CmdKindRunTerminate: func() CommandMessage { return &TerminateCmd{} },
	CmdKindRunEvent:     func() CommandMessage { return &EventCmd{} },
}

// commandWire is the compact wire representation of Command for msgpack
type commandWire struct {
	ID        CmdID     `msgpack:"id"`
	Kind      CmdKind   `msgpack:"k"`
	Msg       []byte    `msgpack:"m"`
	Timestamp time.Time `msgpack:"t"`
}

// DecodeCmd decodes a command message based on its kind
func DecodeCmd(kind CmdKind, data []byte) (CommandMessage, error) {
	factory, ok := commandMessageFactories[kind]
	if !ok {
		return nil, fmt.Errorf("unknown command kind: %s", kind)
	}

	msg := factory()
	if err := msgpack.Unmarshal(data, msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// EncodeMsgpack implements custom msgpack encoding for Command
func (c *Command) EncodeMsgpack(enc *msgpack.Encoder) error {
	if c.ID == "" {
		return enc.Encode(&commandWire{})
	}
	if c.Msg == nil {
		return fmt.Errorf("command message cannot be nil")
	}

	msgBytes, err := msgpack.Marshal(c.Msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	wire := commandWire{
		ID:        c.ID,
		Kind:      c.Kind,
		Msg:       msgBytes,
		Timestamp: c.Timestamp,
	}

	return enc.Encode(&wire)
}

// DecodeMsgpack implements custom msgpack decoding for Command
func (c *Command) DecodeMsgpack(dec *msgpack.Decoder) error {
	var wire commandWire
	if err := dec.Decode(&wire); err != nil {
		return fmt.Errorf("failed to decode command: %w", err)
	}

	if wire.ID == "" {
		*c = Command{}
		return nil
	}

	msg, err := DecodeCmd(wire.Kind, wire.Msg)
	if err != nil {
		return fmt.Errorf("failed to decode command message for kind %s: %w", wire.Kind, err)
	}

	*c = Command{
		ID:        wire.ID,
		Kind:      wire.Kind,
		Msg:       msg,
		Timestamp: wire.Timestamp,
	}

	return nil
}
