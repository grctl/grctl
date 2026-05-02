package external

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/vmihailenco/msgpack/v5"
)

type BackgroundTaskSuite struct {
	suite.Suite
}

func TestBackgroundTask(t *testing.T) {
	suite.Run(t, new(BackgroundTaskSuite))
}

func (s *BackgroundTaskSuite) TestNewPurgeRunResidueTask_RoundTrip() {
	parentID := DirectiveID("parent-directive-01")
	wfID := WFID("wf-abc-123")

	task, err := NewPurgeRunResidueTask(parentID, wfID)
	require.NoError(s.T(), err)

	s.Equal(BackgroundTaskKindPurgeRunResidue, task.Kind)
	s.Equal(DeriveBgTaskID(parentID, BackgroundTaskKindPurgeRunResidue), task.DeduplicationID)

	var got PurgeRunResiduePayload
	require.NoError(s.T(), msgpack.Unmarshal(task.Payload, &got))
	s.Equal(wfID, got.WFID)
}

func (s *BackgroundTaskSuite) TestNewPurgeRunResidueTask_DeduplicationID() {
	parentA := DirectiveID("parent-a")
	parentB := DirectiveID("parent-b")
	wfID := WFID("wf-xyz")

	taskA1, err := NewPurgeRunResidueTask(parentA, wfID)
	require.NoError(s.T(), err)
	taskA2, err := NewPurgeRunResidueTask(parentA, wfID)
	require.NoError(s.T(), err)
	taskB, err := NewPurgeRunResidueTask(parentB, wfID)
	require.NoError(s.T(), err)

	s.Equal(taskA1.DeduplicationID, taskA2.DeduplicationID, "same parentID should produce same deduplication ID")
	s.NotEqual(taskA1.DeduplicationID, taskB.DeduplicationID, "different parentID should produce different deduplication ID")
}
