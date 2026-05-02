//go:build integration

package api_test

import (
	"context"
	"testing"
	"time"

	"grctl/server/api"
	"grctl/server/config"
	"grctl/server/natsreg"
	"grctl/server/store"
	"grctl/server/testutil"
	ext "grctl/server/types/external/v1"

	"grctl/server/server"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/suite"
	"github.com/vmihailenco/msgpack/v5"
)

type HandleStartCmdTestSuite struct {
	suite.Suite
	nc         *nats.Conn
	ns         *natsserver.Server
	srv        *server.Server
	stateStore *store.StateStore
}

func (s *HandleStartCmdTestSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc = nc
	s.ns = ns

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			WorkerResponseTimeout: 5 * time.Second,
			StepTimeout:           5 * time.Minute,
		},
	}

	srv, err := server.NewServer(context.Background(), nc, js, cfg, &server.Options{InMemory: true})
	s.Require().NoError(err)

	err = srv.Start()
	s.Require().NoError(err)
	s.srv = srv

	stream, err := store.EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)
	s.stateStore = store.NewStateStore(js, stream)
}

func (s *HandleStartCmdTestSuite) TearDownTest() {
	_ = s.srv.Stop(context.Background())
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func TestHandleStartCmd(t *testing.T) {
	suite.Run(t, new(HandleStartCmdTestSuite))
}

func (s *HandleStartCmdTestSuite) TestDescribeRun_ActiveWorkflow() {
	ctx := context.Background()

	wfID := ext.NewWFID()
	runID := ext.NewRunID()
	wfType := ext.NewWFType("test-workflow")

	// Start a workflow first
	startCmd := ext.Command{
		ID:        ext.NewCmdID(),
		Kind:      ext.CmdKindRunStart,
		Timestamp: time.Now().UTC(),
		Msg: &ext.StartCmd{
			RunInfo: ext.RunInfo{
				ID:     runID,
				WFID:   wfID,
				WFType: wfType,
			},
		},
	}
	data, err := msgpack.Marshal(&startCmd)
	s.Require().NoError(err)
	subject := natsreg.Manifest.APISubject(wfID)
	_, err = s.nc.Request(subject, data, 3*time.Second)
	s.Require().NoError(err)

	// Verify run was created
	_, _, err = s.stateStore.GetRunByWFID(ctx, wfID)
	s.Require().NoError(err)

	// Now describe the workflow
	describeCmd := ext.Command{
		ID:        ext.NewCmdID(),
		Kind:      ext.CmdKindRunDescribe,
		Timestamp: time.Now().UTC(),
		Msg:       &ext.DescribeCmd{WFID: wfID},
	}
	data, err = msgpack.Marshal(&describeCmd)
	s.Require().NoError(err)

	msg, err := s.nc.Request(subject, data, 3*time.Second)
	s.Require().NoError(err)

	var resp api.GrctlAPIResponse
	err = msgpack.Unmarshal(msg.Data, &resp)
	s.Require().NoError(err)
	s.Require().True(resp.Success, "expected success response, got error: %v", resp.Error)
	s.Require().NotNil(resp.Payload)

	// Decode the RunInfo from payload
	payloadBytes, err := msgpack.Marshal(resp.Payload)
	s.Require().NoError(err)
	var runInfo ext.RunInfo
	err = msgpack.Unmarshal(payloadBytes, &runInfo)
	s.Require().NoError(err)
	s.Equal(wfID, runInfo.WFID)
	s.Equal(runID, runInfo.ID)
	s.Equal(wfType, runInfo.WFType)
}

func (s *HandleStartCmdTestSuite) TestDescribeRun_UnknownWorkflow() {
	unknownWFID := ext.NewWFID()

	describeCmd := ext.Command{
		ID:        ext.NewCmdID(),
		Kind:      ext.CmdKindRunDescribe,
		Timestamp: time.Now().UTC(),
		Msg:       &ext.DescribeCmd{WFID: unknownWFID},
	}
	data, err := msgpack.Marshal(&describeCmd)
	s.Require().NoError(err)

	subject := natsreg.Manifest.APISubject(unknownWFID)
	msg, err := s.nc.Request(subject, data, 3*time.Second)
	s.Require().NoError(err)

	var resp api.GrctlAPIResponse
	err = msgpack.Unmarshal(msg.Data, &resp)
	s.Require().NoError(err)
	s.Require().False(resp.Success)
	s.Require().NotNil(resp.Error)
	s.Equal(4002, resp.Error.Code)
}

func (s *HandleStartCmdTestSuite) TestRunStartIsProcessed() {
	ctx := context.Background()

	wfID := ext.NewWFID()
	runID := ext.NewRunID()
	wfType := ext.NewWFType("test-workflow")

	cmd := ext.Command{
		ID:        ext.NewCmdID(),
		Kind:      ext.CmdKindRunStart,
		Timestamp: time.Now().UTC(),
		Msg: &ext.StartCmd{
			RunInfo: ext.RunInfo{
				ID:     runID,
				WFID:   wfID,
				WFType: wfType,
			},
		},
	}

	data, err := msgpack.Marshal(&cmd)
	s.Require().NoError(err)

	subject := natsreg.Manifest.APISubject(wfID)
	msg, err := s.nc.Request(subject, data, 3*time.Second)
	s.Require().NoError(err)

	var resp api.GrctlAPIResponse
	err = msgpack.Unmarshal(msg.Data, &resp)
	s.Require().NoError(err)
	s.Require().True(resp.Success, "expected success response, got error: %v", resp.Error)

	runInfo, _, err := s.stateStore.GetRunByWFID(ctx, wfID)
	s.Require().NoError(err)
	s.Equal(wfID, runInfo.WFID)
	s.Equal(runID, runInfo.ID)

	s.Require().Eventually(func() bool {
		state, err := s.stateStore.GetRunState(ctx, wfID, runID)
		if err != nil {
			return false
		}
		return state.Kind == ext.RunStateStep
	}, 2*time.Second, 50*time.Millisecond, "expected RunState to transition to RunStateStep")

	history, err := s.stateStore.GetHistoryForRun(ctx, wfID, runID)
	s.Require().NoError(err)
	s.Require().Len(history, 1)
	s.Equal(ext.HistoryKindRunStarted, history[0].Kind)
}
