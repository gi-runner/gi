package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gi-runner/gi/internal/spec"
	"github.com/spf13/cobra"
)

func newSpecCommand(code *int) *cobra.Command {
	var out string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "spec [spec directory]",
		Short: "Write gi features from a Spec Kit, Kiro, or OpenSpec directory",
		Example: "  gi spec --json specs/001-payment\n" +
			"  gi check specs/001-payment/features\n" +
			"  gi test specs/001-payment/features --var baseUrl=http://127.0.0.1:8080",
		Args: cobra.MaximumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			*code = runSpec(path, out, jsonOut)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "Feature directory (default: <spec>/features)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the files and notes as JSON")
	return cmd
}

func runSpec(path, out string, jsonOut bool) int {
	result, err := spec.Compile(path, out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	} else {
		for _, file := range result.Files {
			fmt.Printf("wrote %s (%d scenarios)\n", file.Path, file.Scenarios)
		}
		for _, note := range result.Notes {
			fmt.Printf("%s: %s\n", note.Kind, note.Text)
		}
	}
	if len(result.Files) == 0 {
		return 1
	}
	return 0
}
