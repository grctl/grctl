package jsstore

import (
	"context"
	"errors"
	"fmt"
	"grctl/server/natsreg"
	models "grctl/server/types"
	ext "grctl/server/types/external/v1"
	"log/slog"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/synadia-io/orbit.go/jetstreamext"
	"github.com/vmihailenco/msgpack/v5"
)

var ErrRunStateNotFound = errors.New("run state not found")

type JSStateStore struct {
	js     jetstream.JetStream
	stream jetstream.Stream
}

func NewJSStateStore(js jetstream.JetStream, stream jetstream.Stream) *JSStateStore {
	return &JSStateStore{
		js:     js,
		stream: stream,
	}
}

// Create stores a new workflow run.
// Returns models.ErrWorkflowAlreadyRunning if there's already an active non-terminal run.
func (s *JSStateStore) CreateRunInfo(ctx context.Context, info ext.RunInfo) error {
	// Check if there's an active run for this workflow ID, regardless of run ID.
	ri, _, err := s.GetRunByWFID(ctx, info.WFID)
	if err != nil && !errors.Is(err, models.ErrWorkflowRunNotFound) {
		return fmt.Errorf("failed to check running status: %w", err)
	}

	if err == nil && !ri.IsTerminal() {
		return fmt.Errorf("%w: %s", models.ErrWorkflowAlreadyRunning, ri.ID)
	}

	// Save the run record
	data, err := msgpack.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal workflow run info: %w", err)
	}

	key := natsreg.Manifest.RunInfoKey(info.WFType, info.WFID, info.ID)
	if _, err := s.js.Publish(ctx, key, data); err != nil {
		return fmt.Errorf("failed to save workflow run: %w", err)
	}

	return nil
}

// Get retrieves a workflow run by workflow type, workflow ID and run ID.
func (s *JSStateStore) GetRunInfo(ctx context.Context, WFType ext.WFType, WFID ext.WFID, runID ext.RunID) (ext.RunInfo, error) {
	key := natsreg.Manifest.RunInfoKey(WFType, WFID, runID)
	entry, err := s.stream.GetLastMsgForSubject(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return ext.RunInfo{}, models.ErrWorkflowRunNotFound
		}
		return ext.RunInfo{}, fmt.Errorf("failed to get workflow run: %w", err)
	}

	var info ext.RunInfo
	if err := msgpack.Unmarshal(entry.Data, &info); err != nil {
		return ext.RunInfo{}, fmt.Errorf("failed to unmarshal workflow run info: %w", err)
	}

	return info, nil
}

func (s *JSStateStore) GetRunByWFID(ctx context.Context, WFID ext.WFID) (ext.RunInfo, uint64, error) {
	subject := natsreg.Manifest.ListRunInfoByWFIDPattern("*", WFID)
	msg, err := s.stream.GetLastMsgForSubject(ctx, subject)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return ext.RunInfo{}, 0, models.ErrWorkflowRunNotFound
		}
		return ext.RunInfo{}, 0, fmt.Errorf("failed to get batch iterator: %w", err)
	}

	var info ext.RunInfo
	if err := msgpack.Unmarshal(msg.Data, &info); err != nil {
		return ext.RunInfo{}, 0, fmt.Errorf("failed to unmarshal workflow run info: %w", err)
	}

	return info, msg.Sequence, nil
}

func (s *JSStateStore) GetRunByRunID(ctx context.Context, RunID ext.RunID) (ext.RunInfo, error) {
	subject := natsreg.Manifest.ListRunInfoByRunIDPattern(RunID)
	msg, err := s.stream.GetLastMsgForSubject(ctx, subject)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return ext.RunInfo{}, models.ErrWorkflowRunNotFound
		}
		return ext.RunInfo{}, fmt.Errorf("failed to get batch iterator: %w", err)
	}

	var info ext.RunInfo
	if err := msgpack.Unmarshal(msg.Data, &info); err != nil {
		return ext.RunInfo{}, fmt.Errorf("failed to unmarshal workflow run info: %w", err)
	}

	return info, nil
}

func (s *JSStateStore) ListRuns(ctx context.Context) ([]*ext.RunInfo, error) {
	subject := natsreg.Manifest.RunInfoKey("*", "*", "*")
	slog.Debug("listing workflow runs with subject pattern", "subject", subject)
	streamName := natsreg.Manifest.StateStreamName()
	msgs, err := jetstreamext.GetLastMsgsFor(ctx, s.js, streamName, []string{subject})
	if err != nil {
		return nil, fmt.Errorf("failed to get batch iterator: %w", err)
	}

	var runs []*ext.RunInfo
	for msg, err := range msgs {
		if err != nil {
			slog.Error("listRuns iteration error", "error", err)
			continue
		}

		var info ext.RunInfo
		if err := msgpack.Unmarshal(msg.Data, &info); err != nil {
			slog.Error("listRuns unmarshal error", "error", err)
			continue
		}
		runs = append(runs, &info)
	}
	return runs, nil
}

func (s *JSStateStore) GetStateSnapshot(ctx context.Context, wfID ext.WFID, runID ext.RunID) (models.StateSnapshot, error) {
	var snapshot models.StateSnapshot

	state, err := s.GetRunState(ctx, wfID, runID)
	if err != nil && !errors.Is(err, ErrRunStateNotFound) {
		return models.StateSnapshot{}, err
	}
	if err == nil {
		snapshot.RunState = state
	}

	event, _, err := s.GetNextEvent(ctx, wfID, snapshot.RunState.LastEventSeqID)
	if err != nil {
		if !errors.Is(err, jetstreamext.ErrNoMessages) {
			return models.StateSnapshot{}, err
		}
	} else {
		snapshot.Event = event
	}

	return snapshot, nil
}

func (s *JSStateStore) GetRunState(ctx context.Context, wfID ext.WFID, runID ext.RunID) (ext.RunState, error) {
	subject := natsreg.Manifest.RunStateSubject(wfID, runID)
	entry, err := s.stream.GetLastMsgForSubject(ctx, subject)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return ext.RunState{}, ErrRunStateNotFound
		}
		return ext.RunState{}, fmt.Errorf("failed to get run state: %w", err)
	}

	var state ext.RunState
	if err := msgpack.Unmarshal(entry.Data, &state); err != nil {
		return ext.RunState{}, fmt.Errorf("failed to unmarshal run state: %w", err)
	}

	state.SeqID = entry.Sequence
	return state, nil
}

func (s *JSStateStore) GetNextEvent(ctx context.Context, wfID ext.WFID, startAfterSeq uint64) (ext.Directive, uint64, error) {
	subject := natsreg.Manifest.EventInboxSubject(wfID)
	opts := []jetstreamext.GetBatchOpt{jetstreamext.GetBatchSubject(subject)}
	if startAfterSeq > 0 {
		opts = append(opts, jetstreamext.GetBatchSeq(startAfterSeq+1))
	}
	msgs, err := jetstreamext.GetBatch(ctx, s.js, natsreg.Manifest.StateStreamName(), 1, opts...)
	if err != nil {
		return ext.Directive{}, 0, fmt.Errorf("failed to get next event: %w", err)
	}

	for msg, err := range msgs {
		if err != nil {
			return ext.Directive{}, 0, fmt.Errorf("failed to read event batch: %w", err)
		}

		var d ext.Directive
		if err := msgpack.Unmarshal(msg.Data, &d); err != nil {
			return ext.Directive{}, 0, fmt.Errorf("failed to unmarshal event: %w", err)
		}

		d.Msg.(*ext.Event).EventSeqID = &msg.Sequence

		return d, msg.Sequence, nil
	}

	return ext.Directive{}, 0, jetstreamext.ErrNoMessages
}

func (h *JSStateStore) GetHistoryForRun(ctx context.Context, workflowID ext.WFID, runID ext.RunID) ([]*ext.HistoryEvent, error) {
	subject := natsreg.Manifest.HistorySubject(workflowID, runID)
	slog.Debug("fetching history for run", "subject", subject)
	streamName := natsreg.Manifest.StateStreamName()
	msgs, err := jetstreamext.GetBatch(ctx, h.js, streamName, 10000, jetstreamext.GetBatchSubject(subject))
	if err != nil {
		return nil, fmt.Errorf("failed to get history messages: %w", err)
	}

	var events []*ext.HistoryEvent
	for msg, err := range msgs {
		if err != nil {
			slog.Debug("getHistoryForRun iteration error", "error", err)
			continue
		}

		var event ext.HistoryEvent
		if err := msgpack.Unmarshal(msg.Data, &event); err != nil {
			slog.Debug("getHistoryForRun unmarshal error", "error", err)
			continue
		}
		events = append(events, &event)
	}

	slog.Debug("listed workflow runs", "count", len(events))

	return events, nil
}

func (s *JSStateStore) HasRunInput(ctx context.Context, wfID ext.WFID, runID ext.RunID) (bool, error) {
	key := natsreg.Manifest.RunInputKey(wfID, runID)
	_, err := s.stream.GetLastMsgForSubject(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check run input: %w", err)
	}
	return true, nil
}

func (s *JSStateStore) HasRunOutput(ctx context.Context, wfID ext.WFID, runID ext.RunID) (bool, error) {
	key := natsreg.Manifest.RunOutputKey(wfID, runID)
	_, err := s.stream.GetLastMsgForSubject(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check run output: %w", err)
	}
	return true, nil
}

func (s *JSStateStore) HasRunError(ctx context.Context, wfID ext.WFID, runID ext.RunID) (bool, error) {
	key := natsreg.Manifest.RunErrorKey(wfID, runID)
	_, err := s.stream.GetLastMsgForSubject(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrMsgNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check run error: %w", err)
	}
	return true, nil
}

func (s *JSStateStore) DeleteInboxEvent(ctx context.Context, seqID uint64) error {
	if err := s.stream.DeleteMsg(ctx, seqID); err != nil {
		return fmt.Errorf("failed to delete inbox event with seqID %d: %w", seqID, err)
	}
	return nil
}

func (s *JSStateStore) PurgeRunResidue(ctx context.Context, wfID ext.WFID) error {
	patterns := []string{
		natsreg.Manifest.DirectivePurgePattern(wfID),
		natsreg.Manifest.TimerPurgePattern(wfID),
		natsreg.Manifest.EventInboxPurgePattern(wfID),
		natsreg.Manifest.WorkerTaskPurgePattern(wfID),
	}
	for _, pattern := range patterns {
		if err := s.stream.Purge(ctx, jetstream.WithPurgeSubject(pattern)); err != nil {
			return fmt.Errorf("failed to purge subject %s: %w", pattern, err)
		}
	}
	return nil
}

func (s *JSStateStore) PublishDirective(ctx context.Context, d ext.Directive) error {
	subject := natsreg.Manifest.DirectiveSubject(d.RunInfo.WFType, d.RunInfo.WFID, d.RunInfo.ID)

	// msgpack must see a pointer to call Directive's custom EncodeMsgpack.
	data, err := msgpack.Marshal(&d)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Set Nats-Msg-Id for JetStream deduplication: if the server crashes after publish
	// but before ACKing the caller, redelivery produces a duplicate that NATS drops.
	msg := nats.NewMsg(subject)
	msg.Data = data
	msg.Header.Set(nats.MsgIdHdr, string(d.ID))

	if _, err = s.js.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish directive: %w", err)
	}

	return nil
}

func (s *JSStateStore) Commit(ctx context.Context, records []models.Record) (models.CommitResult, error) {
	if len(records) < 1 {
		return models.CommitResult{}, fmt.Errorf("transition requires at least 1 record, got %d", len(records))
	}

	var msgs []nats.Msg
	for _, record := range records {
		recordMsgs, err := recordToNats(record)
		if err != nil {
			return models.CommitResult{}, fmt.Errorf("failed to create NATS messages: %w", err)
		}
		msgs = append(msgs, recordMsgs...)
	}

	if len(msgs) == 0 {
		return models.CommitResult{}, fmt.Errorf("no messages to publish")
	}

	batch, err := jetstreamext.NewBatchPublisher(s.js)
	if err != nil {
		return models.CommitResult{}, fmt.Errorf("failed to create batch publisher: %w", err)
	}

	for i := 0; i < len(msgs)-1; i++ {
		if err = batch.AddMsg(&msgs[i]); err != nil {
			return models.CommitResult{}, fmt.Errorf("failed to add message to batch: %w", err)
		}
	}

	if _, err = batch.CommitMsg(ctx, &msgs[len(msgs)-1]); err != nil {
		subjects := make([]string, 0, len(msgs))
		for _, m := range msgs {
			subjects = append(subjects, m.Subject)
		}
		return models.CommitResult{
			IsCASRejection:     IsCASRejection(err),
			IsDuplicateMessage: IsDuplicateMessage(err),
		}, fmt.Errorf("failed to commit batch (last_subject=%s, subjects=%s): %w", msgs[len(msgs)-1].Subject, strings.Join(subjects, ","), err)
	}

	return models.CommitResult{}, nil
}

// IsCASRejection reports whether err is a NATS OCC sequence-mismatch rejection.
// This is distinct from infrastructure errors (connection loss, timeout) which should be retried.
func IsCASRejection(err error) bool {
	var apiErr *jetstream.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence
}

// IsDuplicateMessage reports whether err indicates a duplicate message-id publish.
// NATS may surface this as explicit duplicate-message API errors (including newer
// server error codes not yet defined in nats.go constants).
func IsDuplicateMessage(err error) bool {
	var apiErr *jetstream.APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	// 10201 is returned by newer servers for atomic-batch duplicate Msg-Id.
	if apiErr.ErrorCode == 10201 {
		return true
	}

	return strings.Contains(strings.ToLower(apiErr.Description), "duplicate message")
}
