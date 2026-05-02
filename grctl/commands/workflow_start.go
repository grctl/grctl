package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"grctl/server/api"
	"grctl/server/natsreg"
	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/vmihailenco/msgpack/v5"
)

var startFlagType string
var startFlagID string
var startFlagInput string
var startFlagInputFile string

var workflowStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a workflow run",
	Example: `  grctl workflow start --type OrderFulfillment --input '{"orderId": "12345"}'
  grctl workflow start --type OrderFulfillment --input-file ./payload.json
  grctl workflow start --type OrderFulfillment --id my-order-123 --input '{"orderId": "12345"}'`,
	RunE: runWorkflowStart,
}

func init() {
	workflowStartCmd.Flags().StringVarP(&startFlagType, "type", "t", "", "Workflow type (required)")
	workflowStartCmd.Flags().StringVarP(&startFlagID, "id", "i", "", "Workflow ID (auto-generated if not provided)")
	workflowStartCmd.Flags().StringVarP(&startFlagInput, "input", "I", "", "JSON input for the workflow")
	workflowStartCmd.Flags().StringVar(&startFlagInputFile, "input-file", "", "Path to a JSON file containing workflow input")

	if err := workflowStartCmd.MarkFlagRequired("type"); err != nil {
		panic(err)
	}
	workflowStartCmd.MarkFlagsMutuallyExclusive("input", "input-file")
}

func parseWorkflowInput(inputFlag, inputFileFlag string) (*any, error) {
	if inputFlag == "" && inputFileFlag == "" {
		return nil, nil
	}

	if inputFlag != "" && inputFileFlag != "" {
		return nil, fmt.Errorf("cannot use both --input and --input-file")
	}

	var rawJSON []byte

	if inputFlag != "" {
		rawJSON = []byte(inputFlag)
		var v any
		if err := json.Unmarshal(rawJSON, &v); err != nil {
			return nil, fmt.Errorf("invalid JSON input: %w", err)
		}
		return &v, nil
	}

	data, err := os.ReadFile(inputFileFlag)
	if err != nil {
		return nil, fmt.Errorf("failed to read input file: %w", err)
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("invalid JSON in input file: %w", err)
	}
	return &v, nil
}

func runWorkflowStart(cmd *cobra.Command, args []string) error {
	setupLogging()

	input, err := parseWorkflowInput(startFlagInput, startFlagInputFile)
	if err != nil {
		return err
	}

	wfType := ext.NewWFType(startFlagType)

	var wfID ext.WFID
	if startFlagID != "" {
		wfID = ext.WFID(startFlagID)
	} else {
		wfID = ext.NewWFID()
	}

	runID := ext.NewRunID()

	command := ext.Command{
		ID:        ext.NewCmdID(),
		Kind:      ext.CmdKindRunStart,
		Timestamp: time.Now().UTC(),
		Msg: &ext.StartCmd{
			RunInfo: ext.RunInfo{
				ID:     runID,
				WFID:   wfID,
				WFType: wfType,
			},
			Input: input,
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

	fmt.Println("Workflow started")
	fmt.Printf("  Type:   %s\n", wfType)
	fmt.Printf("  ID:     %s\n", wfID)
	fmt.Printf("  Run ID: %s\n", runID)

	return nil
}
