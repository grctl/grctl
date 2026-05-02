package natsreg

import (
	"testing"

	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/suite"
)

type ManifestSuite struct {
	suite.Suite
}

func TestManifest(t *testing.T) {
	suite.Run(t, new(ManifestSuite))
}

func (s *ManifestSuite) TestDirectivePurgePattern() {
	wfID := ext.WFID("wf-abc")
	s.Equal("grctl_directive.*.wf-abc.>", Manifest.DirectivePurgePattern(wfID))
}

func (s *ManifestSuite) TestTimerPurgePattern() {
	wfID := ext.WFID("wf-abc")
	s.Equal("grctl_timers.wf-abc.>", Manifest.TimerPurgePattern(wfID))
}

func (s *ManifestSuite) TestCancelInboxPurgePattern() {
	wfID := ext.WFID("wf-abc")
	s.Equal("grctl_cancel.wf-abc", Manifest.CancelInboxPurgePattern(wfID))
}

func (s *ManifestSuite) TestEventInboxPurgePattern() {
	wfID := ext.WFID("wf-abc")
	s.Equal("grctl_events.wf-abc", Manifest.EventInboxPurgePattern(wfID))
}

func (s *ManifestSuite) TestWorkerTaskPurgePattern() {
	wfID := ext.WFID("wf-abc")
	s.Equal("grctl_worker_task.*.wf-abc.>", Manifest.WorkerTaskPurgePattern(wfID))
}
