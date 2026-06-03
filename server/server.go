package server

import (
	"context"
	"fmt"

	"grctl/server/api"
	"grctl/server/config"
	"grctl/server/ingress"
	"grctl/server/jsstore"
	"grctl/server/run"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const maxTimerDeliveries = 10
const maxBgTaskDeliveries = 5

type timerSvc interface {
	Start(ingress.TimerHandler) error
	Stop()
}

type directiveSvc interface {
	Start(ingress.DirectiveHandler) error
	Stop()
}

type bgTaskSvc interface {
	Start(ingress.BgTaskHandler) error
	Stop()
}

type Server struct {
	nc              *nats.Conn
	senderID        string
	apiSubscriber   *api.APISubscriber
	directiveQueue  directiveSvc
	runManager      *run.Manager
	timerStream     timerSvc
	timerMsgHandler *run.TimerMsgHandler
	bgTaskQueue     bgTaskSvc
	bgTaskHandler   *run.BgTaskHandler
}

// Options configures the server.
type Options struct {
	InMemory bool // Use in-memory storage for streams and KV buckets
}

func NewServer(
	ctx context.Context,
	nc *nats.Conn,
	js jetstream.JetStream,
	cfg *config.Config,
	opts *Options,
) (*Server, error) {
	if opts == nil {
		opts = &Options{}
	}

	stateStream, err := jsstore.EnsureStateStream(ctx, js, opts.InMemory)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state stream: %w", err)
	}

	stateStore := jsstore.NewJSStateStore(js, stateStream)

	directiveQueue, err := ingress.NewDirectiveQueue(ctx, js, stateStream)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize directive queue: %w", err)
	}

	timerStream, err := ingress.NewTimerStream(ctx, js, stateStream)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize timer stream: %w", err)
	}

	senderID, err := deriveServerID(cfg)
	if err != nil {
		return nil, fmt.Errorf("derive server ID: %w", err)
	}

	// srv is created early so it can be passed as WorkerCommandPublisher to bgTaskHandler.
	srv := &Server{nc: nc, senderID: senderID}

	runManager := run.NewManager(stateStore)
	timerMsgHandler := run.NewTimerMsgHandler(stateStore, maxTimerDeliveries)
	bgTaskHandler := run.NewBgTaskHandler(timerStream, stateStore, stateStore, stateStore, srv, maxBgTaskDeliveries)

	bgTaskQueue, err := ingress.NewBgTaskQueue(ctx, stateStream)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize background task queue: %w", err)
	}

	registry := jsstore.NewWorkflowTypeRegistry(js, stateStream)
	runAPI := run.NewService(stateStore, &cfg.Defaults, registry)
	apiHandler := api.NewAPIHandler(runAPI)
	apiSubscriber := api.NewAPISubscriber(nc, apiHandler)

	srv.apiSubscriber = apiSubscriber
	srv.directiveQueue = directiveQueue
	srv.runManager = runManager
	srv.timerStream = timerStream
	srv.timerMsgHandler = timerMsgHandler
	srv.bgTaskQueue = bgTaskQueue
	srv.bgTaskHandler = bgTaskHandler

	return srv, nil
}

func (s *Server) Start() error {
	if err := s.directiveQueue.Start(s.runManager.Handle); err != nil {
		return fmt.Errorf("failed to start directive queue: %w", err)
	}

	if err := s.apiSubscriber.Start(); err != nil {
		return fmt.Errorf("failed to start API handler: %w", err)
	}

	if err := s.timerStream.Start(s.timerMsgHandler.Handle); err != nil {
		return fmt.Errorf("failed to start timer consumer: %w", err)
	}

	if err := s.bgTaskQueue.Start(s.bgTaskHandler.Handle); err != nil {
		return fmt.Errorf("failed to start background task consumer: %w", err)
	}

	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.apiSubscriber.Stop()
		s.directiveQueue.Stop()
		s.timerStream.Stop()
		s.bgTaskQueue.Stop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown timed out: %w", ctx.Err())
	}
}
