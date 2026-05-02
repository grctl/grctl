package server

import (
	"context"
	"fmt"

	"grctl/server/api"
	"grctl/server/config"
	"grctl/server/ingress"
	"grctl/server/machine"
	"grctl/server/store"

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
	apiSubscriber    *api.APISubscriber
	directiveQueue   directiveSvc
	directiveHandler *machine.DirectiveHandler
	timerStream      timerSvc
	timerMsgHandler  *machine.TimerMsgHandler
	bgTaskQueue      bgTaskSvc
	bgTaskHandler    *machine.BgTaskHandler
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

	stateStream, err := store.EnsureStateStream(ctx, js, opts.InMemory)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state stream: %w", err)
	}

	stateStore := store.NewStateStore(js, stateStream)

	directiveQueue, err := ingress.NewDirectiveQueue(ctx, js, stateStream)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize directive queue: %w", err)
	}

	timerStream, err := ingress.NewTimerStream(ctx, js, stateStream)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize timer stream: %w", err)
	}

	directiveHandler := machine.NewDirectiveHandler(stateStore)
	timerMsgHandler := machine.NewTimerMsgHandler(stateStore, maxTimerDeliveries)
	bgTaskHandler := machine.NewBgTaskHandler(timerStream, stateStore, stateStore, maxBgTaskDeliveries)

	bgTaskQueue, err := ingress.NewBgTaskQueue(ctx, stateStream)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize background task queue: %w", err)
	}

	runAPI := machine.NewRunAPI(stateStore, &cfg.Defaults)
	apiHandler := api.NewAPIHandler(runAPI)
	apiSubscriber := api.NewAPISubscriber(nc, apiHandler)

	return &Server{
		apiSubscriber:    apiSubscriber,
		directiveQueue:   directiveQueue,
		directiveHandler: directiveHandler,
		timerStream:      timerStream,
		timerMsgHandler:  timerMsgHandler,
		bgTaskQueue:      bgTaskQueue,
		bgTaskHandler:    bgTaskHandler,
	}, nil
}

func (s *Server) Start() error {
	if err := s.directiveQueue.Start(s.directiveHandler.Handle); err != nil {
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
