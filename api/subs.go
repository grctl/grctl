package api

import (
	"fmt"
	"grctl/server/natsreg"
	"grctl/server/types/external/v1"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

type GrctlAPIResponse struct {
	Success bool        `json:"success" msgpack:"success"`
	Payload any         `json:"payload,omitempty" msgpack:"payload,omitempty"`
	Error   *grctlError `json:"error,omitempty" msgpack:"error,omitempty"`
}

type APISubscriber struct {
	nc         *nats.Conn
	apiHandler *APIHandler
	sub        *nats.Subscription
}

func NewAPISubscriber(nc *nats.Conn, apiHandler *APIHandler) *APISubscriber {
	return &APISubscriber{nc: nc, apiHandler: apiHandler}
}

func (s *APISubscriber) Start() error {
	apiSubj := natsreg.Manifest.APISubjectListener()

	sub, err := s.nc.Subscribe(apiSubj, s.handler)
	if err != nil {
		return fmt.Errorf("failed to subscribe to API subject %s: %w", apiSubj, err)
	}
	s.sub = sub
	return nil
}

func (s *APISubscriber) Stop() {
	if s.sub != nil {
		err := s.sub.Drain()
		if err != nil {
			slog.Error("failed to drain API subscription", "error", err)
		}
	}
}

func (s *APISubscriber) respondWithError(msg *nats.Msg, grctlErr *grctlError) {
	errRes := GrctlAPIResponse{Success: false, Error: grctlErr}
	resBytes, err := msgpack.Marshal(errRes)
	if err != nil {
		slog.Error("failed to marshal error response", "error", err)
		return
	}
	if err := msg.Respond(resBytes); err != nil {
		slog.Error("failed to send error response", "error", err)
	}
}

func (s *APISubscriber) handler(msg *nats.Msg) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in API handler", "panic", r)
			errRes := GrctlAPIResponse{
				Success: false,
				Error:   &grctlError{Message: fmt.Sprintf("internal error: %v", r)},
			}
			if resBytes, err := msgpack.Marshal(errRes); err == nil {
				if err := msg.Respond(resBytes); err != nil {
					slog.Error("failed to send panic response", "error", err)
				}
			} else {
				slog.Error("failed to marshal panic response", "error", err)
			}
		}
	}()

	var c external.Command
	if err := msgpack.Unmarshal(msg.Data, &c); err != nil {
		slog.Error("failed to unmarshal command", "error", err)
		grctlErr := NewgrctlError(err)
		s.respondWithError(msg, &grctlErr)
		return
	}

	slog.Debug("Received command", "command", c)

	payload, err := s.apiHandler.handleMessage(c)
	if err != nil {
		slog.Error("failed to handle command", "error", err, "kind", c.Kind)
		grctlErr := NewgrctlError(err)
		s.respondWithError(msg, &grctlErr)
		return
	}

	res := GrctlAPIResponse{Success: true, Payload: payload}
	resBytes, err := msgpack.Marshal(res)
	if err != nil {
		slog.Error("failed to marshal success response", "error", err)
		grctlErr := NewgrctlError(err)
		s.respondWithError(msg, &grctlErr)
		return
	}

	slog.Debug("Responding to client", "response", res)
	if err := msg.Respond(resBytes); err != nil {
		slog.Error("failed to send response", "error", err)
	}
}
