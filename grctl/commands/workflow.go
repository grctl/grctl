package commands

import (
	"grctl/server/grctl/tui/workflow_browser"
	"grctl/server/grctl/tui/workflow_history"
	ext "grctl/server/types/external/v1"

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

var workflowHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show history for a workflow run",
	Long:  `Display the event history of a specific workflow run in an interactive table view.`,
	Example: `  grctl workflow history --id my-order-123
  grctl workflow history --id my-order-123 --run-id 01JQKZ2XYZ`,
	RunE: runWorkflowHistory,
}

func init() {
	workflowHistoryCmd.Flags().StringVarP(&historyFlagWFID, "id", "i", "", "Workflow ID (required)")
	workflowHistoryCmd.Flags().StringVar(&historyFlagID, "run-id", "", "Workflow run ID (optional, defaults to latest run)")
	if err := workflowHistoryCmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}

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
	cleanup := setupFileLogging()
	defer cleanup()

	wfID := ext.WFID(historyFlagWFID)
	runID := ext.RunID(historyFlagID)

	m := workflow_history.NewWorkflowHistoryModel(normalizeServerURL(serverURL), wfID, runID, 0, 0)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return err
	}

	return nil
}
