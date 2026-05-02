package machine

import (
	"testing"

	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type HistoryBldSuite struct {
	suite.Suite
	b *HistoryBuilder
}

func (s *HistoryBldSuite) SetupTest() {
	s.b = NewHistoryBuilder()
}

func TestHistoryBld(t *testing.T) {
	suite.Run(t, new(HistoryBldSuite))
}

// --- helpers ---

func makeStepResultDirective(stepName string) ext.Directive {
	workerID := ext.WorkerID("worker-1")
	return ext.Directive{
		ID:   ext.NewDirectiveID(),
		Kind: ext.DirectiveKindStepResult,
		RunInfo: ext.RunInfo{
			WFID:   ext.NewWFID(),
			ID:     ext.NewRunID(),
			WFType: ext.NewWFType("test"),
		},
		Msg: &ext.StepResult{
			ProcessedMsgKind: ext.DirectiveKindStep,
			ProcessedMsg:     &ext.Step{Name: stepName, Timeout: 1000},
			WorkerID:         &workerID,
			DurationMS:       42,
			NextMsgKind:      ext.DirectiveKindComplete,
			NextMsg:          &ext.Complete{},
		},
	}
}

// --- StepCompleted ---

func (s *HistoryBldSuite) TestStepCompleted() {
	d := makeStepResultDirective("my-step")

	event, err := s.b.StepCompleted(d)
	require.NoError(s.T(), err)
	require.Equal(s.T(), ext.HistoryKindStepCompleted, event.Kind)

	msg, ok := event.Msg.(ext.StepCompleted)
	require.True(s.T(), ok, "expected Msg to be ext.StepCompleted, got %T", event.Msg)
	require.Equal(s.T(), "my-step", msg.StepName)
	require.NotZero(s.T(), msg.DurationMS)
	require.NotEmpty(s.T(), msg.WorkerID)
}

func (s *HistoryBldSuite) TestStepCompleted_ErrorOnWrongMsgType() {
	d := makeStepDirective()
	d.Kind = ext.DirectiveKindStepResult

	_, err := s.b.StepCompleted(d)
	require.Error(s.T(), err)
}
