package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"grctl/server/grctl/tui/workflow_history"
	ext "grctl/server/types/external/v1"
)

// resolveRunIDs resolves a fully-qualified (wfID, runID) pair from partial input.
// When only runID is provided, wfID is looked up via the server.
// When only wfID is provided, the latest runID is resolved.
func resolveRunIDs(serverURL, rawWFID, rawRunID string) (ext.WFID, ext.RunID, error) {
	wfID := ext.WFID(rawWFID)
	runID := ext.RunID(rawRunID)

	loader, err := workflow_history.NewHistoryLoader(serverURL)
	if err != nil {
		return "", "", err
	}
	defer loader.Close()

	return resolveRunIDsWithLoader(context.Background(), loader, wfID, runID)
}

func resolveRunIDsWithLoader(ctx context.Context, loader *workflow_history.HistoryLoader, wfID ext.WFID, runID ext.RunID) (ext.WFID, ext.RunID, error) {
	if wfID == "" && runID != "" {
		info, err := loader.GetRunInfoByRunID(ctx, runID)
		if err != nil {
			return "", "", fmt.Errorf("resolve run %s: %w", runID, err)
		}
		return info.WFID, info.ID, nil
	}

	if runID == "" {
		info, err := loader.GetLatestRunInfo(ctx, wfID)
		if err != nil {
			return "", "", fmt.Errorf("resolve latest run for workflow %s: %w", wfID, err)
		}
		return wfID, info.ID, nil
	}

	return wfID, runID, nil
}

// printWorkflowHistoryJSON fetches history for the given run and writes JSONL to stdout,
// sorted chronologically ascending (oldest event first).
func printWorkflowHistoryJSON(serverURL, rawWFID, rawRunID string) error {
	loader, err := workflow_history.NewHistoryLoader(serverURL)
	if err != nil {
		return err
	}
	defer loader.Close()

	ctx := context.Background()
	wfID := ext.WFID(rawWFID)
	runID := ext.RunID(rawRunID)

	wfID, runID, err = resolveRunIDsWithLoader(ctx, loader, wfID, runID)
	if err != nil {
		return err
	}

	events, err := loader.GetHistory(ctx, wfID, runID)
	if err != nil {
		return fmt.Errorf("fetch history: %w", err)
	}

	// GetHistory returns descending; re-sort ascending for scripting use.
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		fmt.Println(string(line))
	}

	return nil
}
