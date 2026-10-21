package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func Main(args []string) int {
	code := 0
	root := newCommand(&code)
	root.SetArgs(args)
	if len(args) == 0 {
		_ = root.Help()
		return 2
	}
	if command, err := root.ExecuteC(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprint(os.Stderr, command.UsageString())
		return 2
	}
	return code
}

func newCommand(code *int) *cobra.Command {
	root := &cobra.Command{
		Use: "gi", Short: "Run feature files as functional tests or load tests",
		Version: version, SilenceErrors: true, SilenceUsage: true,
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.SetVersionTemplate("gi {{.Version}}\n")
	root.Flags().BoolP("version", "v", false, "Print version")
	root.AddCommand(newTestCommand(code), newLoadCommand(code), newMockCommand(code))
	root.AddCommand(&cobra.Command{
		Use: "version", Short: "Print version", Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) { cmd.Println("gi", version) },
	})
	return root
}

type commonOptions struct {
	env, tag                                            string
	vars                                                []string
	timeout                                             time.Duration
	reportPath, junitPath, jsonPath, cucumberPath, otel string
}

func (opt *commonOptions) flags(fs *pflag.FlagSet) {
	fs.StringVar(&opt.env, "env", "", "Load environment configuration and select @env tags")
	fs.StringVar(&opt.tag, "tag", "", "Select scenarios using a tag expression")
	fs.StringArrayVar(&opt.vars, "var", nil, "Set a key=value variable (repeatable)")
	fs.DurationVar(&opt.timeout, "timeout", 30*time.Second, "Per-request timeout")
	fs.StringVar(&opt.reportPath, "report", "", "Write a self-contained HTML report")
	fs.StringVar(&opt.junitPath, "junit", "", "Write a JUnit XML report")
	fs.StringVar(&opt.jsonPath, "json", "", "Write a JSON report")
	fs.StringVar(&opt.cucumberPath, "cucumber", "", "Write a Cucumber JSON report")
	fs.StringVar(&opt.otel, "otel-endpoint", "", "Send traces to an OTLP/HTTP JSON endpoint")
}

type testOptions struct {
	commonOptions
	jobs int
}

func newTestCommand(code *int) *cobra.Command {
	opt := testOptions{}
	cmd := &cobra.Command{
		Use: "test [files or directories]", Short: "Run functional tests",
		Run: func(_ *cobra.Command, args []string) { *code = runTest(args, opt) },
	}
	opt.flags(cmd.Flags())
	cmd.Flags().IntVar(&opt.jobs, "jobs", 0, "Features to run concurrently (default: CPU count; 1 keeps order)")
	return cmd
}

type loadCommandOptions struct {
	commonOptions
	loadOptions
	thresholds []string
}

func newLoadCommand(code *int) *cobra.Command {
	opt := loadCommandOptions{}
	cmd := &cobra.Command{
		Use: "load [files or directories]", Short: "Run load tests",
		Example: "  gi load users.feature --vus 10 --duration 30s\n  gi load users.feature --rate 100 --threshold 'p95 < 200ms'",
		Run:     func(_ *cobra.Command, args []string) { *code = runLoad(args, opt) },
	}
	fs := cmd.Flags()
	opt.fs = fs
	opt.flags(fs)
	fs.IntVar(&opt.vus, "vus", 0, "Virtual users (default: feature setting or 10)")
	fs.IntVar(&opt.vusTo, "vus-to", 0, "Ramp concurrent users to this count")
	fs.Float64Var(&opt.rate, "rate", 0, "Scenario starts per second")
	fs.Float64Var(&opt.rateTo, "rate-to", 0, "Ramp scenario starts per second to this rate")
	fs.StringArrayVar(&opt.stages, "stage", nil, "Load segment, e.g. users=1:40 duration=30s (repeatable)")
	fs.IntVar(&opt.maxIn, "max-inflight", 0, "Open-loop concurrency cap; excess starts are missed")
	fs.IntVar(&opt.maxEndpoints, "max-endpoints", 0, "Named endpoint limit (default: feature setting or 1000)")
	fs.DurationVar(&opt.duration, "duration", 0, "Measured window (default: feature setting or 10s)")
	fs.DurationVar(&opt.warmup, "warmup", 0, "Unmeasured time before the window")
	fs.IntVar(&opt.rps, "rps", 0, "HTTP requests per second cap; 0 means no cap")
	fs.StringArrayVar(&opt.thresholds, "threshold", nil, "Pass condition, e.g. p95 < 200ms (repeatable)")
	return cmd
}

func newMockCommand(code *int) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use: "mock <file.feature>", Short: "Serve a feature as an HTTP mock", Args: cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) { *code = runMock(args, addr) },
	}
	cmd.Flags().StringVar(&addr, "addr", ":18080", "Listen address")
	return cmd
}
