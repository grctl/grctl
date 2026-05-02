package commands

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"grctl/server/natsreg"
	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/spf13/cobra"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/nats-io/nats.go"
)

var dumpFlagOutput string

var debugDumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump all messages from the state stream to a JSON file",
	Example: `  grctl debug dump
  grctl debug dump -o /tmp/stream.json`,
	RunE: runDebugDump,
}

func init() {
	debugDumpCmd.Flags().StringVarP(&dumpFlagOutput, "output", "o", "grctl_stream_dump.json", "Output file path")
}

type DumpEntry struct {
	Sequence    uint64            `json:"sequence"`
	Subject     string            `json:"subject"`
	Timestamp   time.Time         `json:"timestamp"`
	Headers     map[string]string `json:"headers,omitempty"`
	Decoded     any               `json:"decoded,omitempty"`
	DecodeError string            `json:"decode_error,omitempty"`
	RawBase64   string            `json:"raw_base64"`
}

func runDebugDump(cmd *cobra.Command, _ []string) error {
	setupLogging()
	ctx := context.Background()

	nc, err := nats.Connect(normalizeServerURL(serverURL))
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}

	streamName := natsreg.Manifest.StateStreamName()

	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return fmt.Errorf("failed to bind stream %q: %w", streamName, err)
	}

	info, err := stream.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get stream info: %w", err)
	}
	slog.Info("starting stream dump", "stream", streamName, "messages", info.State.Msgs, "output", dumpFlagOutput)

	cons, err := js.OrderedConsumer(ctx, streamName, jetstream.OrderedConsumerConfig{
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to create ordered consumer: %w", err)
	}

	const batchSize = 500
	var entries []DumpEntry

	for {
		batch, err := cons.FetchNoWait(batchSize)
		if err != nil {
			return fmt.Errorf("failed to fetch batch: %w", err)
		}

		count := 0
		for msg := range batch.Messages() {
			meta, err := msg.Metadata()
			if err != nil {
				slog.Warn("failed to get message metadata", "error", err)
				continue
			}

			entry := DumpEntry{
				Sequence:  meta.Sequence.Stream,
				Subject:   msg.Subject(),
				Timestamp: meta.Timestamp,
				Headers:   flattenHeaders(msg.Headers()),
				RawBase64: base64.StdEncoding.EncodeToString(msg.Data()),
			}

			decoded, decodeErr := decodeBySubject(msg.Subject(), msg.Data())
			if decodeErr != nil {
				slog.Warn("failed to decode message", "subject", msg.Subject(), "seq", meta.Sequence.Stream, "error", decodeErr)
				entry.DecodeError = decodeErr.Error()
			} else {
				entry.Decoded = decoded
			}

			entries = append(entries, entry)
			count++
		}

		if count < batchSize {
			break
		}
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal dump entries: %w", err)
	}

	if err := os.WriteFile(dumpFlagOutput, data, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Printf("Dumped %d messages to %s\n", len(entries), dumpFlagOutput)
	return nil
}

func flattenHeaders(h nats.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	flat := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			flat[k] = v[0]
		}
	}
	return flat
}

func decodeBySubject(subject string, data []byte) (any, error) {
	switch {
	case strings.HasPrefix(subject, "grctl_directive."),
		strings.HasPrefix(subject, "grctl_cancel."),
		strings.HasPrefix(subject, "grctl_events."),
		strings.HasPrefix(subject, "grctl_worker_task."):
		var d ext.Directive
		if err := msgpack.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		return d, nil

	case strings.HasPrefix(subject, "grctl_history."):
		var h ext.HistoryEvent
		if err := msgpack.Unmarshal(data, &h); err != nil {
			return nil, err
		}
		return h, nil

	case strings.HasPrefix(subject, "grctl_timers."), subject == "grctl_timers_fired":
		var t ext.Timer
		if err := msgpack.Unmarshal(data, &t); err != nil {
			return nil, err
		}
		return t, nil

	case strings.HasPrefix(subject, "grctl_state.run_state."):
		var s ext.RunState
		if err := msgpack.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		return s, nil

	case subject == "grctl_bg_task":
		var bt ext.BackgroundTask
		if err := msgpack.Unmarshal(data, &bt); err != nil {
			return nil, err
		}
		return bt, nil

	case strings.HasSuffix(subject, ".info"):
		var ri ext.RunInfo
		if err := msgpack.Unmarshal(data, &ri); err != nil {
			return nil, err
		}
		return ri, nil

	case strings.HasSuffix(subject, ".error"):
		var ed ext.ErrorDetails
		if err := msgpack.Unmarshal(data, &ed); err != nil {
			return nil, err
		}
		return ed, nil

	default:
		// .input, .output, grctl_wf_kv.*, and anything unknown: generic decode
		var v any
		if err := msgpack.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
}
