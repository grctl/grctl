package machine

import (
	"context"
	"errors"
	"testing"

	"grctl/server/config"
	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/suite"
)

type fakeRunStore struct {
	runInfo ext.RunInfo
	getErr  error
}

func (f *fakeRunStore) CreateRunInfo(_ context.Context, _ *ext.RunInfo) error {
	return nil
}

func (f *fakeRunStore) GetRunByWFID(_ context.Context, _ ext.WFID) (ext.RunInfo, uint64, error) {
	return f.runInfo, 0, f.getErr
}

func (f *fakeRunStore) PublishDirective(_ context.Context, _ ext.Directive) error {
	return nil
}

type RunAPISuite struct {
	suite.Suite
	store *fakeRunStore
	api   *RunAPI
}

func (s *RunAPISuite) SetupTest() {
	s.store = &fakeRunStore{}
	s.api = NewRunAPI(s.store, &config.DefaultsConfig{})
}

func TestRunAPI(t *testing.T) {
	suite.Run(t, new(RunAPISuite))
}

func (s *RunAPISuite) TestSend_RejectsTerminalRun() {
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

	err := s.api.Send(context.Background(), &cmd)

	s.True(errors.Is(err, ErrRunTerminal))
}

func (s *RunAPISuite) TestCancel_RejectsTerminalRun() {
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

	err := s.api.Cancel(context.Background(), &cmd)

	s.True(errors.Is(err, ErrRunTerminal))
}
