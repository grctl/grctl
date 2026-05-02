package commands

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWorkflowInput(t *testing.T) {
	tests := []struct {
		name          string
		inputFlag     string
		inputFileFlag string
		wantNil       bool
		wantValue     any
		wantErr       string
	}{
		{
			name:      "both empty returns nil",
			wantNil:   true,
			wantValue: nil,
		},
		{
			name:      "valid JSON input flag",
			inputFlag: `{"key": "value"}`,
			wantValue: map[string]any{"key": "value"},
		},
		{
			name:      "invalid JSON input flag",
			inputFlag: `{invalid}`,
			wantErr:   "invalid JSON input:",
		},
		{
			name:          "both flags set returns error",
			inputFlag:     `{"a":1}`,
			inputFileFlag: "somefile.json",
			wantErr:       "cannot use both --input and --input-file",
		},
		{
			name:          "valid JSON input file",
			inputFileFlag: "PLACEHOLDER",
			wantValue:     map[string]any{"file_key": "file_value"},
		},
		{
			name:          "nonexistent input file",
			inputFileFlag: "/tmp/nonexistent-grctl-test-file.json",
			wantErr:       "failed to read input file:",
		},
		{
			name:          "invalid JSON in input file",
			inputFileFlag: "PLACEHOLDER_INVALID",
			wantErr:       "invalid JSON in input file:",
		},
	}

	// Create temp files for file-based tests
	validFile, err := os.CreateTemp("", "grctl-test-*.json")
	require.NoError(t, err)
	defer func() { _ = os.Remove(validFile.Name()) }()
	_, err = validFile.WriteString(`{"file_key": "file_value"}`)
	require.NoError(t, err)
	require.NoError(t, validFile.Close())

	invalidFile, err := os.CreateTemp("", "grctl-test-invalid-*.json")
	require.NoError(t, err)
	defer func() { _ = os.Remove(invalidFile.Name()) }()
	_, err = invalidFile.WriteString(`{not valid json}`)
	require.NoError(t, err)
	require.NoError(t, invalidFile.Close())

	// Patch placeholders with actual temp file paths
	tests[4].inputFileFlag = validFile.Name()
	tests[6].inputFileFlag = invalidFile.Name()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseWorkflowInput(tt.inputFlag, tt.inputFileFlag)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)

			if tt.wantNil {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			assert.Equal(t, tt.wantValue, *result)
		})
	}
}
