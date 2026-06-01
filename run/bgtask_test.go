package run

import (
	"context"
	"errors"
	"testing"

	intr "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/suite"
)

type fakePurger struct {
	called []ext.WFID
	err    error
}

func (f *fakePurger) PurgeRunResidue(_ context.Context, wfID ext.WFID) error {
	f.called = append(f.called, wfID)
	return f.err
}

type fakeNotifier struct {
	parent     ext.RunInfo
	getErr     error
	published  []ext.Directive
	publishErr error
}

func (f *fakeNotifier) GetRunByWFID(_ context.Context, _ ext.WFID) (ext.RunInfo, uint64, error) {
	if f.getErr != nil {
		return ext.RunInfo{}, 0, f.getErr
	}
	return f.parent, 0, nil
}

func (f *fakeNotifier) PublishDirective(_ context.Context, d ext.Directive) error {
	if f.publishErr != nil {
		return f.publishErr
	}
	f.published = append(f.published, d)
	return nil
}

type BgTaskHandlerSuite struct {
	suite.Suite
	purger   *fakePurger
	notifier *fakeNotifier
	handler  *BgTaskHandler
}

func (s *BgTaskHandlerSuite) SetupTest() {
	s.purger = &fakePurger{}
	s.notifier = &fakeNotifier{}
	s.handler = NewBgTaskHandler(nil, nil, s.purger, s.notifier, 3)
}

func TestBgTaskHandler(t *testing.T) {
	suite.Run(t, new(BgTaskHandlerSuite))
}

func (s *BgTaskHandlerSuite) TestPurgeRunResidue_Success() {
	wfID := ext.WFID("wf-abc")
	task, err := ext.NewPurgeRunResidueTask(ext.NewDirectiveID(), wfID)
	s.Require().NoError(err)

	result := s.handler.Handle(context.Background(), task, 1)

	s.Equal(intr.Processed(), result)
	s.Require().Len(s.purger.called, 1)
	s.Equal(wfID, s.purger.called[0])
}

func (s *BgTaskHandlerSuite) TestPurgeRunResidue_MalformedPayload() {
	task := ext.BackgroundTask{
		Kind:    ext.BackgroundTaskKindPurgeRunResidue,
		Payload: []byte{0xFF, 0xFE, 0x00},
	}

	result := s.handler.Handle(context.Background(), task, 1)

	s.Equal(intr.Processed(), result)
	s.Len(s.purger.called, 0)
}

func (s *BgTaskHandlerSuite) TestPurgeRunResidue_PurgerError() {
	s.purger.err = errors.New("nats transient error")
	task, err := ext.NewPurgeRunResidueTask(ext.NewDirectiveID(), ext.WFID("wf-abc"))
	s.Require().NoError(err)

	result := s.handler.Handle(context.Background(), task, 1)

	s.Equal(intr.Retryable(RetryDelay), result)
}

func (s *BgTaskHandlerSuite) TestPurgeRunResidue_Idempotent() {
	wfID := ext.WFID("wf-abc")
	task, err := ext.NewPurgeRunResidueTask(ext.NewDirectiveID(), wfID)
	s.Require().NoError(err)

	result1 := s.handler.Handle(context.Background(), task, 1)
	result2 := s.handler.Handle(context.Background(), task, 2)

	s.Equal(intr.Processed(), result1)
	s.Equal(intr.Processed(), result2)
	s.Len(s.purger.called, 2)
}

func (s *BgTaskHandlerSuite) TestPurgeRunResidue_MaxDeliveriesExceeded() {
	task, err := ext.NewPurgeRunResidueTask(ext.NewDirectiveID(), ext.WFID("wf-abc"))
	s.Require().NoError(err)

	result := s.handler.Handle(context.Background(), task, 4) // > maxDeliveries=3

	s.Equal(intr.Processed(), result)
	s.Len(s.purger.called, 0)
}

func (s *BgTaskHandlerSuite) notifyTask(status ext.RunStatus, result any, errDetails *ext.ErrorDetails) ext.BackgroundTask {
	task, err := ext.NewNotifyParentCompleteTask(ext.NewDirectiveID(), ext.NotifyParentCompletePayload{
		ParentWFID: ext.WFID("parent-wf"),
		StepName:   "on_child_done",
		ChildWFID:  ext.WFID("child-wf"),
		Status:     status,
		Result:     result,
		Error:      errDetails,
	})
	s.Require().NoError(err)
	return task
}

func (s *BgTaskHandlerSuite) TestNotifyParentComplete_PublishesEventToParent() {
	s.notifier.parent = ext.RunInfo{WFID: "parent-wf", Status: ext.RunStatusRunning}
	task := s.notifyTask(ext.RunStatusCompleted, map[string]any{"value": "ok"}, nil)

	result := s.handler.Handle(context.Background(), task, 1)

	s.Equal(intr.Processed(), result)
	s.Require().Len(s.notifier.published, 1)
	d := s.notifier.published[0]
	s.Equal(ext.DirectiveKindEvent, d.Kind)
	s.Equal(ext.WFID("parent-wf"), d.RunInfo.WFID)
	event, ok := d.Msg.(*ext.Event)
	s.Require().True(ok)
	s.Equal("on_child_done", event.EventName)
	payload, ok := event.Payload.(map[string]any)
	s.Require().True(ok)
	s.Equal(ext.RunStatusCompleted, payload["status"])
	s.Equal(map[string]any{"value": "ok"}, payload["result"])
	s.Nil(payload["error"])
}

func (s *BgTaskHandlerSuite) TestNotifyParentComplete_DropsWhenParentTerminal() {
	s.notifier.parent = ext.RunInfo{WFID: "parent-wf", Status: ext.RunStatusCompleted}
	task := s.notifyTask(ext.RunStatusCompleted, nil, nil)

	result := s.handler.Handle(context.Background(), task, 1)

	s.Equal(intr.Processed(), result)
	s.Len(s.notifier.published, 0)
}

func (s *BgTaskHandlerSuite) TestNotifyParentComplete_RetriesOnLookupError() {
	s.notifier.getErr = errors.New("nats transient error")
	task := s.notifyTask(ext.RunStatusCompleted, nil, nil)

	result := s.handler.Handle(context.Background(), task, 1)

	s.Equal(intr.Retryable(RetryDelay), result)
}

func (s *BgTaskHandlerSuite) TestNotifyParentComplete_RetriesOnPublishError() {
	s.notifier.parent = ext.RunInfo{WFID: "parent-wf", Status: ext.RunStatusRunning}
	s.notifier.publishErr = errors.New("nats transient error")
	task := s.notifyTask(ext.RunStatusFailed, nil, &ext.ErrorDetails{Type: "Boom", Message: "kaboom"})

	result := s.handler.Handle(context.Background(), task, 1)

	s.Equal(intr.Retryable(RetryDelay), result)
}

func (s *BgTaskHandlerSuite) TestNotifyParentComplete_MalformedPayload() {
	task := ext.BackgroundTask{
		Kind:    ext.BackgroundTaskKindNotifyParentComplete,
		Payload: []byte{0xFF, 0xFE, 0x00},
	}

	result := s.handler.Handle(context.Background(), task, 1)

	s.Equal(intr.Processed(), result)
	s.Len(s.notifier.published, 0)
}
