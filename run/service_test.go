package run

import (
	"context"
	"errors"
	"testing"

	"grctl/server/config"
	model "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/suite"
)

type fakeRunStore struct {
	runInfo ext.RunInfo
	getErr  error
}

func (f *fakeRunStore) CreateRunInfo(_ context.Context, _ ext.RunInfo) error {
	return nil
}

func (f *fakeRunStore) GetRunByWFID(_ context.Context, _ ext.WFID) (ext.RunInfo, uint64, error) {
	return f.runInfo, 0, f.getErr
}

func (f *fakeRunStore) PublishDirective(_ context.Context, _ ext.Directive) error {
	return nil
}

type ServiceSuite struct {
	suite.Suite
	store *fakeRunStore
	svc   *Service
}

func (s *ServiceSuite) SetupTest() {
	s.store = &fakeRunStore{}
	s.svc = NewService(s.store, &config.DefaultsConfig{})
}

func TestService(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}

func (s *ServiceSuite) TestSend_RejectsTerminalRun() {
	wfID := ext.NewWFID()
	s.store.runInfo = ext.RunInfo{
		WFID:   wfID,
		Status: ext.RunStatusCompleted,
	}

	cmd := ext.Command{
		Msg: &ext.EventCmd{
			WFID:      wfID,
			EventName: "some-event",
		},
	}

	err := s.svc.Send(context.Background(), cmd)

	s.True(errors.Is(err, model.ErrRunTerminal))
}

func (s *ServiceSuite) TestCancel_RejectsTerminalRun() {
	wfID := ext.NewWFID()
	s.store.runInfo = ext.RunInfo{
		WFID:   wfID,
		Status: ext.RunStatusFailed,
	}

	cmd := ext.Command{
		Msg: &ext.CancelCmd{
			WFID:   wfID,
			Reason: "too late",
		},
	}

	err := s.svc.Cancel(context.Background(), cmd)

	s.True(errors.Is(err, model.ErrRunTerminal))
}
