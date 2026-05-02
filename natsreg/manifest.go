package natsreg

import (
	"embed"
	"fmt"
	ext "grctl/server/types/external/v1"
	"log"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed nats_manifest.yaml
var manifestFS embed.FS

// manifestConfig represents the root YAML structure of nats_manifest.yaml
type manifestConfig struct {
	Version  int                      `yaml:"version"`
	Streams  map[string]streamConfig  `yaml:"streams"`
	Subjects map[string]subjectConfig `yaml:"subjects"`
}

// streamConfig represents a NATS stream configuration
type streamConfig struct {
	Name      string                    `yaml:"name"`
	Type      string                    `yaml:"type"`
	Consumers map[string]consumerConfig `yaml:"consumers"`
	Consumer  map[string]consumerConfig `yaml:"consumer"`
}

// consumerConfig represents a NATS consumer configuration
type consumerConfig struct {
	Name    string `yaml:"name"`
	Subject string `yaml:"subject"`
}

// subjectConfig represents a subject configuration
type subjectConfig struct {
	Stream          *string           `yaml:"stream"` // nullable
	SubjectPattern  string            `yaml:"subject_pattern,omitempty"`
	ListenerPattern string            `yaml:"listener_pattern,omitempty"`
	Patterns        map[string]string `yaml:"subject_patterns,omitempty"`
}

// NATSManifest provides centralized access to NATS configuration
// defined in nats_manifest.yaml (embedded in the binary). It parses
// the manifest at package initialization and provides type-safe methods
// for accessing stream names, subject patterns, bucket names, and key patterns.
type NATSManifest struct {
	config *manifestConfig
}

// Manifest is the singleton instance of NATSManifest, initialized at startup
var Manifest *NATSManifest

func init() {
	var err error
	Manifest, err = loadManifest()
	if err != nil {
		log.Fatalf("Failed to load NATS manifest: %v", err)
	}
}

// loadManifest reads and parses the embedded nats_manifest.yaml file
func loadManifest() (*NATSManifest, error) {
	// Read from embedded file system
	data, err := manifestFS.ReadFile("nats_manifest.yaml")
	if err != nil {
		return nil, fmt.Errorf("reading embedded manifest file: %w", err)
	}

	var config manifestConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing manifest YAML: %w", err)
	}

	return &NATSManifest{config: &config}, nil
}

// substituteParams replaces {placeholder} with actual values in a pattern string
func substituteParams(pattern string, params map[string]string) string {
	result := pattern
	for key, value := range params {
		result = strings.ReplaceAll(result, "{"+key+"}", value)
	}
	return result
}

func (m *NATSManifest) streamConsumers(streamKey string) map[string]consumerConfig {
	stream, ok := m.config.Streams[streamKey]
	if !ok {
		return nil
	}
	if len(stream.Consumers) > 0 {
		return stream.Consumers
	}
	return stream.Consumer
}

func (m *NATSManifest) subjectPattern(subjectKey, patternKey string) string {
	subject, ok := m.config.Subjects[subjectKey]
	if !ok {
		return ""
	}
	if len(subject.Patterns) > 0 {
		return subject.Patterns[patternKey]
	}
	// Backward compatibility with old manifest format.
	switch patternKey {
	case "publish":
		return subject.SubjectPattern
	case "listen":
		return subject.ListenerPattern
	default:
		return ""
	}
}

func (m *NATSManifest) subjectStream(subjectKey string) string {
	subject, ok := m.config.Subjects[subjectKey]
	if !ok || subject.Stream == nil {
		return ""
	}
	return *subject.Stream
}

// DirectiveStreamName returns the name of the directive stream
func (m *NATSManifest) DirectiveStreamName() string {
	return m.subjectStream("directive")
}

// StateStreamName returns the name of the consolidated state stream.
func (m *NATSManifest) StateStreamName() string {
	return m.config.Streams["state"].Name
}

// DirectiveConsumerName returns the consumer name for processing directives.
func (m *NATSManifest) DirectiveConsumerName() string {
	return m.streamConsumers("state")["grctl_directive"].Name
}

// DirectiveSubject constructs a directive subject with the given parameters.
// Pattern: grctl_directive.{wf_type}.{wf_id}.{run_id}
func (m *NATSManifest) DirectiveSubject(wfType ext.WFType, wfID ext.WFID, runID ext.RunID) string {
	pattern := m.subjectPattern("directive", "publish")
	return substituteParams(pattern, map[string]string{
		"wf_type": string(wfType), "wf_id": wfID.String(), "run_id": runID.String(),
	})
}

// DirectiveWFTypeFilterSubject returns the filter subject for a specific workflow type.
// Pattern: grctl_directive.{wf_type}.>
func (m *NATSManifest) DirectiveWFTypeFilterSubject(wfType ext.WFType) string {
	return fmt.Sprintf("grctl_directive.%s.>", string(wfType))
}

// DirectiveListenerPattern returns the listener pattern for directive subjects
func (m *NATSManifest) DirectiveListenerPattern() string {
	return m.subjectPattern("directive", "listen")
}

// HistorySubject constructs a history subject with the given parameters
// Pattern: grctl_history.{wf_id}.{run_id}
func (m *NATSManifest) HistorySubject(wfID ext.WFID, runID ext.RunID) string {
	pattern := m.subjectPattern("history", "publish")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(), "run_id": runID.String(),
	})
}

// HistoryListenerPattern returns the listener pattern for history subjects.
func (m *NATSManifest) HistoryListenerPattern() string {
	return m.subjectPattern("history", "listen")
}

// APISubject constructs an API subject with the given parameters
// Pattern: grctl_api.workflow.{wf_id}
func (m *NATSManifest) APISubject(wfID ext.WFID) string {
	pattern := m.subjectPattern("api", "publish")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(),
	})
}

func (m *NATSManifest) APISubjectListener() string {
	return m.subjectPattern("api", "listen")
}

// RunInfoKey constructs a run info key with the given parameters
// Pattern: grctl_run.{wf_type}.{wf_id}.{run_id}.info
func (m *NATSManifest) RunInfoKey(wfType ext.WFType, wfID ext.WFID, runID ext.RunID) string {
	pattern := m.subjectPattern("run_store", "info")
	return substituteParams(pattern, map[string]string{
		"wf_type": string(wfType), "wf_id": wfID.String(), "run_id": runID.String(),
	})
}

// ListRunInfoByWFIDPattern returns the prefix for all run info keys for a specific workflow
// Pattern: grctl_run.{wf_type}.{wf_id}.
func (m *NATSManifest) ListRunInfoByWFIDPattern(wfType ext.WFType, wfID ext.WFID) string {
	pattern := m.subjectPattern("run_store", "info")
	// Substitute known parameters, leaving run_id as placeholder
	result := substituteParams(pattern, map[string]string{
		"wf_type": string(wfType),
		"wf_id":   wfID.String(),
		"run_id":  "*",
	})
	return result
}

// ListRunInfoByRunIDPattern returns the prefix for all run info keys for a specific workflow
// Pattern: grctl_run.{wf_type}.{wf_id}.
func (m *NATSManifest) ListRunInfoByRunIDPattern(runID ext.RunID) string {
	pattern := m.subjectPattern("run_store", "info")
	// Substitute known parameters, leaving run_id as placeholder
	result := substituteParams(pattern, map[string]string{
		"wf_type": "*",
		"wf_id":   "*",
		"run_id":  runID.String(),
	})
	return result
}

// RunInputKey constructs a run input key with the given parameters
// Pattern: grctl_run.{wf_id}.{run_id}.input
func (m *NATSManifest) RunInputKey(wfID ext.WFID, runID ext.RunID) string {
	pattern := m.subjectPattern("run_store", "input")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(), "run_id": runID.String(),
	})
}

// RunOutputKey constructs a run output key with the given parameters
// Pattern: grctl_run.{wf_id}.{run_id}.output
func (m *NATSManifest) RunOutputKey(wfID ext.WFID, runID ext.RunID) string {
	pattern := m.subjectPattern("run_store", "output")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(), "run_id": runID.String(),
	})
}

// WfKVPrefix returns the prefix for all workflow KV keys for a specific workflow and run
func (m *NATSManifest) WfKVPrefix(wfID ext.WFID) string {
	pattern := m.subjectPattern("kv_store", "kv")
	prefix := substituteParams(pattern, map[string]string{
		"wf_id":  wfID.String(),
		"run_id": "{run_id}",
		"key":    "{key}",
	})
	return strings.TrimSuffix(prefix, ".{run_id}.{key}")
}

// WfRunKVPrefix returns the prefix for all workflow KV keys for a specific workflow and run
func (m *NATSManifest) WfRunKVPrefix(wfID ext.WFID, runID ext.RunID) string {
	pattern := m.subjectPattern("kv_store", "kv")
	prefix := substituteParams(pattern, map[string]string{
		"wf_id":  wfID.String(),
		"run_id": runID.String(),
		"key":    "{key}",
	})
	return strings.TrimSuffix(prefix, ".{key}")
}

// WfKVKey constructs a workflow KV key with the given parameters
// Pattern: grctl_wf_kv.{wf_id}.{run_id}.{key}
func (m *NATSManifest) WfKVKey(wfID ext.WFID, runID ext.RunID, key string) string {
	pattern := m.subjectPattern("kv_store", "kv")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(), "run_id": runID.String(), "key": key,
	})
}

// TimerStreamName returns the name of the timer stream
func (m *NATSManifest) TimerStreamName() string {
	return m.subjectStream("timer")
}

// TimerSubject constructs a timer subject with the given parameters
// Pattern: grctl_timers.{wf_id}.{kind}.{timer_id}
func (m *NATSManifest) TimerSubject(wfID ext.WFID, kind ext.TimerKind, timerID ext.TimerID) string {
	pattern := m.subjectPattern("timer", "publish")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(), "kind": string(kind), "timer_id": string(timerID),
	})
}

// TimerFiredSubject returns the subject for fired timers
func (m *NATSManifest) TimerFiredSubject() string {
	return m.subjectPattern("timer_fired", "publish")
}

// TimerFiredConsumerName returns the name of the timer fired consumer
func (m *NATSManifest) TimerFiredConsumerName() string {
	return m.streamConsumers("state")["grctl_timer_fired"].Name
}

// TimerListenerPattern returns the listener pattern for timer subjects
func (m *NATSManifest) TimerListenerPattern() string {
	return m.subjectPattern("timer", "listen")
}

// EventInboxSubject returns the subject for publishing an event for a workflow
func (m *NATSManifest) EventInboxSubject(wfID ext.WFID) string {
	pattern := m.subjectPattern("events", "publish")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(),
	})
}

// EventInboxListenerPattern returns the listener pattern for event inbox subjects
func (m *NATSManifest) EventInboxListenerPattern() string {
	return m.subjectPattern("events", "listen")
}

// RunStateSubject constructs a run state subject with the given parameters
// Pattern: grctl_state.run_state.{wf_id}.{run_id}
func (m *NATSManifest) RunStateSubject(wfID ext.WFID, runID ext.RunID) string {
	pattern := m.subjectPattern("run_state", "publish")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(), "run_id": runID.String(),
	})
}

// RunStateListenerPattern returns the listener pattern for run state subjects
func (m *NATSManifest) RunStateListenerPattern() string {
	return m.subjectPattern("run_state", "listen")
}

// CancelInboxSubject returns the subject for publishing a cancel for a workflow
func (m *NATSManifest) CancelInboxSubject(wfID ext.WFID) string {
	pattern := m.subjectPattern("cancel", "publish")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(),
	})
}

// CancelListenerPattern returns the listener pattern for cancel subjects
func (m *NATSManifest) CancelListenerPattern() string {
	return m.subjectPattern("cancel", "listen")
}

// AllRunsInfoKeyPattern returns the pattern to match all run info keys
// Pattern: grctl_run.*.*.*.info (derived from infos pattern)
func (m *NATSManifest) AllRunsInfoKeyPattern() string {
	pattern := m.subjectPattern("run_store", "info")
	// Replace all parameter placeholders with wildcards
	pattern = strings.ReplaceAll(pattern, "{wf_type}", "*")
	pattern = strings.ReplaceAll(pattern, "{wf_id}", "*")
	pattern = strings.ReplaceAll(pattern, "{run_id}", "*")
	return pattern
}

// AllRunsInputKeyPattern returns the pattern to match all run input keys.
// Pattern: grctl_run.*.*.input
func (m *NATSManifest) AllRunsInputKeyPattern() string {
	pattern := m.subjectPattern("run_store", "input")
	pattern = strings.ReplaceAll(pattern, "{wf_id}", "*")
	pattern = strings.ReplaceAll(pattern, "{run_id}", "*")
	return pattern
}

// AllRunsOutputKeyPattern returns the pattern to match all run output keys.
// Pattern: grctl_run.*.*.output
func (m *NATSManifest) AllRunsOutputKeyPattern() string {
	pattern := m.subjectPattern("run_store", "output")
	pattern = strings.ReplaceAll(pattern, "{wf_id}", "*")
	pattern = strings.ReplaceAll(pattern, "{run_id}", "*")
	return pattern
}

// RunErrorKey constructs a run error key with the given parameters
// Pattern: grctl_run.{wf_id}.{run_id}.error
func (m *NATSManifest) RunErrorKey(wfID ext.WFID, runID ext.RunID) string {
	pattern := m.subjectPattern("run_store", "error")
	return substituteParams(pattern, map[string]string{
		"wf_id": wfID.String(), "run_id": runID.String(),
	})
}

// AllRunsErrorKeyPattern returns the pattern to match all run error keys.
// Pattern: grctl_run.*.*.error
func (m *NATSManifest) AllRunsErrorKeyPattern() string {
	pattern := m.subjectPattern("run_store", "error")
	pattern = strings.ReplaceAll(pattern, "{wf_id}", "*")
	pattern = strings.ReplaceAll(pattern, "{run_id}", "*")
	return pattern
}

// WorkerTaskSubject constructs a worker task subject with the given parameters
// Pattern: grctl_worker_task.{wf_type}.{wf_id}.{run_id}
func (m *NATSManifest) WorkerTaskSubject(wfType ext.WFType, wfID ext.WFID, runID ext.RunID) string {
	pattern := m.subjectPattern("worker_task", "publish")
	return substituteParams(pattern, map[string]string{
		"wf_type": string(wfType), "wf_id": wfID.String(), "run_id": runID.String(),
	})
}

// WorkerTaskListenerPattern returns the listener pattern for worker task subjects
func (m *NATSManifest) WorkerTaskListenerPattern() string {
	return m.subjectPattern("worker_task", "listen")
}

// DirectivePurgePattern returns the purge filter subject for all directives of a workflow run.
// Pattern: grctl_directive.*.{wfID}.>
func (m *NATSManifest) DirectivePurgePattern(wfID ext.WFID) string {
	pattern := m.subjectPattern("directive", "purge")
	return substituteParams(pattern, map[string]string{"wf_id": wfID.String()})
}

// TimerPurgePattern returns the purge filter subject for all timers of a workflow run.
// Pattern: grctl_timers.{wfID}.>
func (m *NATSManifest) TimerPurgePattern(wfID ext.WFID) string {
	pattern := m.subjectPattern("timer", "purge")
	return substituteParams(pattern, map[string]string{"wf_id": wfID.String()})
}

// CancelInboxPurgePattern returns the purge filter subject for the cancel inbox of a workflow run.
// Pattern: grctl_cancel.{wfID}
func (m *NATSManifest) CancelInboxPurgePattern(wfID ext.WFID) string {
	pattern := m.subjectPattern("cancel", "purge")
	return substituteParams(pattern, map[string]string{"wf_id": wfID.String()})
}

// EventInboxPurgePattern returns the purge filter subject for the event inbox of a workflow run.
// Pattern: grctl_events.{wfID}
func (m *NATSManifest) EventInboxPurgePattern(wfID ext.WFID) string {
	pattern := m.subjectPattern("events", "purge")
	return substituteParams(pattern, map[string]string{"wf_id": wfID.String()})
}

// WorkerTaskPurgePattern returns the purge filter subject for all worker tasks of a workflow run.
// Pattern: grctl_worker_task.*.{wfID}.>
func (m *NATSManifest) WorkerTaskPurgePattern(wfID ext.WFID) string {
	pattern := m.subjectPattern("worker_task", "purge")
	return substituteParams(pattern, map[string]string{"wf_id": wfID.String()})
}

// BgTaskSubject returns the subject for publishing background tasks.
func (m *NATSManifest) BgTaskSubject() string {
	return m.subjectPattern("bg_task", "publish")
}

// BgTaskConsumerName returns the consumer name for processing background tasks.
func (m *NATSManifest) BgTaskConsumerName() string {
	return m.streamConsumers("state")["grctl_bg_task"].Name
}

// AllWfKVKeyPattern returns the pattern to match all workflow KV keys.
// Pattern: grctl_wf_kv.*.*.*
func (m *NATSManifest) AllWfKVKeyPattern() string {
	pattern := m.subjectPattern("kv_store", "kv")
	pattern = strings.ReplaceAll(pattern, "{wf_id}", "*")
	pattern = strings.ReplaceAll(pattern, "{run_id}", "*")
	pattern = strings.ReplaceAll(pattern, "{key}", "*")
	return pattern
}
