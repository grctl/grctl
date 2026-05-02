package machine

import (
	"context"
	"testing"

	intr "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/suite"
)

type fakePublisher struct {
	published []ext.Directive
	err       error
}

func (f *fakePublisher) PublishDirective(_ context.Context, d ext.Directive) error {
	f.published = append(f.published, d)
	return f.err
}

type TimerMsgHandlerTestSuite struct {
	suite.Suite
	publisher *fakePublisher
	handler   *TimerMsgHandler
}

func (s *TimerMsgHandlerTestSuite) SetupTest() {
	s.publisher = &fakePublisher{}
	s.handler = NewTimerMsgHandler(s.publisher, 3)
}

func TestTimerMsgHandler(t *testing.T) {
	suite.Run(t, new(TimerMsgHandlerTestSuite))
}

func (s *TimerMsgHandlerTestSuite) TestMaxDeliveriesLeadsToApplyFailure() {
	timer := makeDirectiveTimer(ext.TimerKindStepTimeout)
	result := s.handler.Handle(context.Background(), timer, 4) // > maxDeliveries=3

	s.Equal(intr.Processed(), result)
	s.Require().Len(s.publisher.published, 1)
	d := s.publisher.published[0]
	s.Equal(ext.DirectiveKindFail, d.Kind)
}

func (s *TimerMsgHandlerTestSuite) TestStepTimeoutKindPublishesStepTimeoutDirective() {
	timer := makeDirectiveTimer(ext.TimerKindStepTimeout)
	result := s.handler.Handle(context.Background(), timer, 1)

	s.Equal(intr.Processed(), result)
	s.Require().Len(s.publisher.published, 1)
	d := s.publisher.published[0]
	s.Equal(ext.DirectiveKindStepTimeout, d.Kind)
	msg, ok := d.Msg.(ext.StepTimeout)
	s.True(ok)
	s.Equal("test-step", msg.StepName)
}

func (s *TimerMsgHandlerTestSuite) TestApplyFailurePublishesCorrectCause() {
	timer := makeDirectiveTimer(ext.TimerKindStepTimeout)
	cause := "something went wrong"
	s.handler.applyFailure(context.Background(), timer.Directive, cause)

	s.Require().Len(s.publisher.published, 1)
	d := s.publisher.published[0]
	s.Equal(ext.DirectiveKindFail, d.Kind)
	msg, ok := d.Msg.(*ext.Fail)
	s.True(ok)
	s.Equal(cause, msg.Error.Message)
	s.Equal("TimerExhausted", msg.Error.Type)
}

func (s *TimerMsgHandlerTestSuite) TestStepTimeoutKindWithStartDirective() {
	timer := ext.Timer{
		ID:   "timer-1",
		WFID: "wf-1",
		Kind: ext.TimerKindStepTimeout,
		Directive: ext.Directive{
			ID:   ext.NewDirectiveID(),
			Kind: ext.DirectiveKindStart,
			RunInfo: ext.RunInfo{
				WFID: "wf-1",
				ID:   "run-1",
			},
			Msg: &ext.Start{Input: nil, Timeout: 2000},
		},
	}
	result := s.handler.Handle(context.Background(), timer, 1)

	s.Equal(intr.Processed(), result)
	s.Require().Len(s.publisher.published, 1)
	d := s.publisher.published[0]
	s.Equal(ext.DirectiveKindStepTimeout, d.Kind)
	msg, ok := d.Msg.(ext.StepTimeout)
	s.True(ok)
	s.Equal("start", msg.StepName)
}

func makeDirectiveTimer(kind ext.TimerKind) ext.Timer {
	return ext.Timer{
		ID:   "timer-1",
		WFID: "wf-1",
		Kind: kind,
		Directive: ext.Directive{
			ID:   ext.NewDirectiveID(),
			Kind: ext.DirectiveKindStep,
			RunInfo: ext.RunInfo{
				WFID: "wf-1",
				ID:   "run-1",
			},
			Msg: &ext.Step{Name: "test-step"},
		},
	}
}
