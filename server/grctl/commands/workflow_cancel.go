package commands

import (
	"fmt"
	"time"

	"grctl/server/api"
	"grctl/server/natsreg"
	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/vmihailenco/msgpack/v5"
)

var cancelFlagID string
var cancelFlagReason string

var workflowCancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel a running workflow",
	Example: `  grctl workflow cancel --id my-order-123
  grctl workflow cancel --id my-order-123 --reason "Manual intervention required"`,
	RunE: runWorkflowCancel,
}

func init() {
	workflowCancelCmd.Flags().StringVarP(&cancelFlagID, "id", "i", "", "Workflow ID (required)")
	workflowCancelCmd.Flags().StringVar(&cancelFlagReason, "reason", "", "Cancellation reason")

	if err := workflowCancelCmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}
}

func runWorkflowCancel(cmd *cobra.Command, args []string) error {
	setupLogging()

	wfID := ext.WFID(cancelFlagID)

	command := ext.Command{
		ID:        ext.NewCmdID(),
		Kind:      ext.CmdKindRunCancel,
		Timestamp: time.Now().UTC(),
		Msg: &ext.CancelCmd{
			WFID:   wfID,
			Reason: cancelFlagReason,
		},
	}

	data, err := msgpack.Marshal(&command)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	nc, err := nats.Connect(normalizeServerURL(serverURL))
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer nc.Close()

	subject := natsreg.Manifest.APISubject(wfID)
	msg, err := nc.Request(subject, data, 5*time.Second)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	var resp api.GrctlAPIResponse
	if err := msgpack.Unmarshal(msg.Data, &resp); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("server error: %s", resp.Error.Error())
	}

	fmt.Println("Workflow cancelled")
	fmt.Printf("  ID: %s\n", wfID)

	return nil
}
