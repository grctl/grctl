package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"grctl/server/config"
	"grctl/server/jsstore"
	model "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/suite"
)

type fakeRunStore struct {
	runInfo      ext.RunInfo
	getErr       error
	publishedDir ext.Directive
}

func (f *fakeRunStore) CreateRunInfo(_ context.Context, _ ext.RunInfo) error {
	return nil
}

func (f *fakeRunStore) GetRunByWFID(_ context.Context, _ ext.WFID) (ext.RunInfo, uint64, error) {
	return f.runInfo, 0, f.getErr
}

func (f *fakeRunStore) PublishDirective(_ context.Context, d ext.Directive) error {
	f.publishedDir = d
	return nil
}

type fakeTypeRegistry struct {
	timeoutMS uint32
	getErr    error
	putCalls  []struct {
		workerID string
		defs     []ext.WorkflowTypeDef
	}
}

func (f *fakeTypeRegistry) PutTypes(_ context.Context, workerID string, defs []ext.WorkflowTypeDef) error {
	f.putCalls = append(f.putCalls, struct {
		workerID string
		defs     []ext.WorkflowTypeDef
	}{workerID, defs})
	return nil
}

func (f *fakeTypeRegistry) GetStartStepTimeout(_ context.Context, _ ext.WFType) (uint32, error) {
	return f.timeoutMS, f.getErr
}

func (f *fakeTypeRegistry) GetEventDef(_ context.Context, _ ext.WFType, _ string) (ext.EventDef, error) {
	return ext.EventDef{}, nil
}

type ServiceSuite struct {
	suite.Suite
	store    *fakeRunStore
	registry *fakeTypeRegistry
	svc      *Service
}

func (s *ServiceSuite) SetupTest() {
	s.store = &fakeRunStore{}
	s.registry = &fakeTypeRegistry{}
	s.svc = NewService(s.store, &config.DefaultsConfig{StepTimeout: 5 * time.Minute}, s.registry)
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

func (s *ServiceSuite) TestStartRun_UnregisteredType() {
	s.registry.getErr = jsstore.ErrWorkflowTypeNotRegistered
	cmd := ext.StartCmd{RunInfo: ext.RunInfo{WFType: "unknown"}}

	err := s.svc.StartRun(context.Background(), cmd)

	s.True(errors.Is(err, jsstore.ErrWorkflowTypeNotRegistered))
}

func (s *ServiceSuite) TestStartRun_UsesRegisteredTimeout() {
	s.registry.timeoutMS = 10000
	cmd := ext.StartCmd{RunInfo: ext.RunInfo{WFType: "my-workflow"}}

	err := s.svc.StartRun(context.Background(), cmd)

	s.NoError(err)
	start, ok := s.store.publishedDir.Msg.(*ext.Start)
	s.True(ok)
	s.Equal(uint32(10000), start.Timeout)
}

func (s *ServiceSuite) TestStartRun_FallsBackToConfigTimeout() {
	s.registry.timeoutMS = 0
	cmd := ext.StartCmd{RunInfo: ext.RunInfo{WFType: "my-workflow"}}

	err := s.svc.StartRun(context.Background(), cmd)

	s.NoError(err)
	start, ok := s.store.publishedDir.Msg.(*ext.Start)
	s.True(ok)
	s.Equal(uint32(5*time.Minute.Milliseconds()), start.Timeout)
}

func (s *ServiceSuite) TestRegister_DelegatesToRegistry() {
	defs := []ext.WorkflowTypeDef{{Type: "wf-a"}}
	cmd := ext.RegisterCmd{WorkerID: "worker-1", Types: defs}

	err := s.svc.Register(context.Background(), cmd)

	s.NoError(err)
	s.Len(s.registry.putCalls, 1)
	s.Equal("worker-1", s.registry.putCalls[0].workerID)
	s.Equal(defs, s.registry.putCalls[0].defs)
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

func (s *ServiceSuite) TestTerminate_RejectsTerminalRun() {
	wfID := ext.NewWFID()
	s.store.runInfo = ext.RunInfo{
		WFID:   wfID,
		Status: ext.RunStatusCompleted,
	}

	cmd := ext.Command{
		Msg: &ext.TerminateCmd{
			WFID:   wfID,
			Reason: "too late",
		},
	}

	err := s.svc.Terminate(context.Background(), cmd)

	s.True(errors.Is(err, model.ErrRunTerminal))
}

func (s *ServiceSuite) TestTerminate_PublishesTerminateDirective() {
	wfID := ext.NewWFID()
	s.store.runInfo = ext.RunInfo{
		WFID:   wfID,
		Status: ext.RunStatusRunning,
	}

	cmd := ext.Command{
		Msg: &ext.TerminateCmd{
			WFID:   wfID,
			Reason: "stop it",
		},
	}

	err := s.svc.Terminate(context.Background(), cmd)

	s.NoError(err)
	s.Equal(ext.DirectiveKindTerminate, s.store.publishedDir.Kind)
	terminate, ok := s.store.publishedDir.Msg.(*ext.Terminate)
	s.True(ok)
	s.Equal("stop it", terminate.Reason)
}
