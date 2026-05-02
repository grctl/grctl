package machine

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

type BgTaskHandlerSuite struct {
	suite.Suite
	purger  *fakePurger
	handler *BgTaskHandler
}

func (s *BgTaskHandlerSuite) SetupTest() {
	s.purger = &fakePurger{}
	s.handler = NewBgTaskHandler(nil, nil, s.purger, 3)
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

	s.Equal(intr.Retryable(NackDelay), result)
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
