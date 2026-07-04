package commands

import (
	"fmt"
	"grctl/server/grctl/tui/workflow_browser"
	"grctl/server/grctl/tui/workflow_history"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Workflow management commands",
}

var workflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workflow runs",
	Long:  `Display all workflow runs in an interactive table view.`,
	RunE:  runWorkflowList,
}

var historyFlagWFID string
var historyFlagID string

var historyFlagJSON bool

var workflowHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show history for a workflow run",
	Long:  `Display the event history of a specific workflow run in an interactive table view, or print JSONL to stdout with --json.`,
	Example: `  grctl workflow history --id my-order-123
  grctl workflow history --run-id 01JQKZ2XYZ
  grctl workflow history --id my-order-123 --json
  grctl workflow history --run-id 01JQKZ2XYZ --json | jq .kind`,
	RunE: runWorkflowHistory,
}

func init() {
	workflowHistoryCmd.Flags().StringVarP(&historyFlagWFID, "id", "i", "", "Workflow ID")
	workflowHistoryCmd.Flags().StringVar(&historyFlagID, "run-id", "", "Workflow run ID (defaults to latest run)")
	workflowHistoryCmd.Flags().BoolVar(&historyFlagJSON, "json", false, "Output history as JSONL to stdout")

	workflowCmd.AddCommand(workflowListCmd)
	workflowCmd.AddCommand(workflowHistoryCmd)
	workflowCmd.AddCommand(workflowStartCmd)
	workflowCmd.AddCommand(workflowCancelCmd)
}

func runWorkflowList(cmd *cobra.Command, args []string) error {
	cleanup := setupFileLogging()
	defer cleanup()

	m := workflow_browser.NewWorkflowBrowserModel(normalizeServerURL(serverURL))
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}

func runWorkflowHistory(cmd *cobra.Command, args []string) error {
	if historyFlagWFID == "" && historyFlagID == "" {
		return fmt.Errorf("at least one of --id or --run-id is required")
	}

	if historyFlagJSON {
		return printWorkflowHistoryJSON(normalizeServerURL(serverURL), historyFlagWFID, historyFlagID)
	}

	cleanup := setupFileLogging()
	defer cleanup()

	wfID, runID, err := resolveRunIDs(normalizeServerURL(serverURL), historyFlagWFID, historyFlagID)
	if err != nil {
		return err
	}

	m := workflow_history.NewWorkflowHistoryModel(normalizeServerURL(serverURL), wfID, runID, 0, 0)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}
