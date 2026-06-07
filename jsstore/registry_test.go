//go:build integration

package jsstore

import (
	"context"
	"errors"
	"testing"

	"grctl/server/natsreg"
	"grctl/server/testutil"
	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/suite"
)

type RegistryTestSuite struct {
	suite.Suite
	nc       *nats.Conn
	ns       *server.Server
	registry *WorkflowTypeRegistry
}

func (s *RegistryTestSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc, s.ns = nc, ns

	if natsreg.Manifest == nil {
		s.Require().NoError(natsreg.Init())
	}

	stream, err := EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)

	s.registry = NewWorkflowTypeRegistry(js, stream)
}

func (s *RegistryTestSuite) TearDownTest() {
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func TestRegistry(t *testing.T) {
	suite.Run(t, new(RegistryTestSuite))
}

func orderType() ext.WorkflowTypeDef {
	return ext.WorkflowTypeDef{
		Type:      ext.WFType("order_wf"),
		StartStep: "start",
		Steps:     []string{"reserve", "charge"},
		Events:    []ext.EventDef{{Name: "approve"}},
		Queries:   []string{"status"},
	}
}

func (s *RegistryTestSuite) TestPutTypes_PersistsAndDirectGet() {
	ctx := context.Background()
	def := orderType()

	s.Require().NoError(s.registry.PutTypes(ctx, "w:worker-1", []ext.WorkflowTypeDef{def}))

	entry, err := s.registry.GetType(ctx, def.Type)
	s.Require().NoError(err)
	s.Equal(def, entry.WorkflowTypeDef)
	s.Equal("w:worker-1", entry.WorkerID)
	s.False(entry.RegisteredAt.IsZero())
}

func (s *RegistryTestSuite) TestGetType_UnregisteredReturnsNotFound() {
	_, err := s.registry.GetType(context.Background(), ext.WFType("never_registered"))
	s.Require().ErrorIs(err, ErrWorkflowTypeNotRegistered)
}

func (s *RegistryTestSuite) TestPutTypes_ReRegistrationOverwrites() {
	ctx := context.Background()
	def := orderType()

	s.Require().NoError(s.registry.PutTypes(ctx, "w:worker-1", []ext.WorkflowTypeDef{def}))

	updated := def
	updated.Steps = []string{"reserve", "charge", "ship"}
	s.Require().NoError(s.registry.PutTypes(ctx, "w:worker-2", []ext.WorkflowTypeDef{updated}))

	entry, err := s.registry.GetType(ctx, def.Type)
	s.Require().NoError(err)
	s.Equal(updated, entry.WorkflowTypeDef)
	s.Equal("w:worker-2", entry.WorkerID)

	// Rollup keeps exactly one message for the subject.
	entries, err := s.registry.ListTypes(ctx)
	s.Require().NoError(err)
	s.Len(entries, 1)
}

func (s *RegistryTestSuite) TestPutTypes_MultipleTypesAllPersist() {
	ctx := context.Background()
	defs := []ext.WorkflowTypeDef{
		orderType(),
		{Type: ext.WFType("payment_wf"), StartStep: "begin", Steps: []string{"authorize"}},
		{Type: ext.WFType("shipping_wf"), StartStep: "pickup"},
	}

	s.Require().NoError(s.registry.PutTypes(ctx, "w:worker-1", defs))

	entries, err := s.registry.ListTypes(ctx)
	s.Require().NoError(err)
	s.Len(entries, len(defs))
}

func (s *RegistryTestSuite) TestPutTypes_AtomicBatchLeavesZeroOnFailure() {
	ctx := context.Background()
	// A workflow type containing whitespace yields an invalid NATS subject,
	// causing the atomic batch to be rejected. No partial catalog must remain.
	defs := []ext.WorkflowTypeDef{
		{Type: ext.WFType("valid_first"), StartStep: "start"},
		{Type: ext.WFType("invalid type with spaces"), StartStep: "start"},
		{Type: ext.WFType("valid_last"), StartStep: "start"},
	}

	err := s.registry.PutTypes(ctx, "w:worker-1", defs)
	s.Require().Error(err)

	entries, err := s.registry.ListTypes(ctx)
	s.Require().NoError(err)
	s.Empty(entries)
}

func (s *RegistryTestSuite) TestPutTypes_EmptyCatalogWritesNothing() {
	ctx := context.Background()

	s.Require().NoError(s.registry.PutTypes(ctx, "w:worker-1", nil))

	entries, err := s.registry.ListTypes(ctx)
	s.Require().NoError(err)
	s.Empty(entries)
}

func (s *RegistryTestSuite) TestRegistry_GetEventDef_ReturnsZeroForUnknownEvent() {
	ctx := context.Background()
	def := ext.WorkflowTypeDef{
		Type:   ext.WFType("order_wf"),
		Events: []ext.EventDef{{Name: "approve"}},
	}

	s.Require().NoError(s.registry.PutTypes(ctx, "w:worker-1", []ext.WorkflowTypeDef{def}))

	ed, err := s.registry.GetEventDef(ctx, def.Type, "nonexistent")
	s.Require().NoError(err)
	s.Equal("", ed.Name)
	s.Equal(uint32(0), ed.TimeoutMS)
}

func (s *RegistryTestSuite) TestRegistry_GetEventDef_ReturnsErrorForUnregisteredType() {
	ctx := context.Background()

	_, err := s.registry.GetEventDef(ctx, ext.WFType("unknown"), "ev")
	s.Require().ErrorIs(err, ErrWorkflowTypeNotRegistered)
}

// TestPutTypes_SurvivesServerRestart writes to a file-backed stream, restarts
// the NATS server against the same store directory, and confirms the entry is
// still readable without re-registration.
func TestPutTypes_SurvivesServerRestart(t *testing.T) {
	storeDir := t.TempDir()
	ctx := context.Background()
	def := orderType()

	if natsreg.Manifest == nil {
		if err := natsreg.Init(); err != nil {
			t.Fatalf("failed to init nats manifest: %v", err)
		}
	}

	nc, js, ns, err := testutil.RunEmbeddedNATS(storeDir)
	if err != nil {
		t.Fatalf("failed to start NATS: %v", err)
	}

	stream, err := EnsureStateStream(ctx, js, false)
	if err != nil {
		t.Fatalf("failed to ensure stream: %v", err)
	}

	registry := NewWorkflowTypeRegistry(js, stream)
	if err := registry.PutTypes(ctx, "w:worker-1", []ext.WorkflowTypeDef{def}); err != nil {
		t.Fatalf("failed to put types: %v", err)
	}

	nc.Close()
	ns.Shutdown()
	ns.WaitForShutdown()

	nc2, js2, ns2, err := testutil.RunEmbeddedNATS(storeDir)
	if err != nil {
		t.Fatalf("failed to restart NATS: %v", err)
	}
	defer func() {
		nc2.Close()
		ns2.Shutdown()
		ns2.WaitForShutdown()
	}()

	stream2, err := EnsureStateStream(ctx, js2, false)
	if err != nil {
		t.Fatalf("failed to ensure stream after restart: %v", err)
	}

	registry2 := NewWorkflowTypeRegistry(js2, stream2)
	entry, err := registry2.GetType(ctx, def.Type)
	if err != nil {
		t.Fatalf("expected entry to survive restart, got error: %v", err)
	}
	if entry.WorkerID != "w:worker-1" {
		t.Fatalf("expected worker id w:worker-1, got %s", entry.WorkerID)
	}
	if errors.Is(err, ErrWorkflowTypeNotRegistered) {
		t.Fatal("entry was lost across restart")
	}
}
