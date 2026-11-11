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
	root.AddCommand(newTestCommand(code), newLoadCommand(code), newMockCommand(code), newCheckCommand(code), newSpecCommand(code), newDemoCommand(code), newInitCommand(code))
	root.AddCommand(&cobra.Command{
		Use: "version", Short: "Print version", Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) { cmd.Println("gi", version) },
	})
	return root
}

func newCheckCommand(code *int) *cobra.Command {
	return &cobra.Command{
		Use: "check [files or directories]", Short: "Report steps, configure keys, and tags gi cannot run, without running them",
		Example: "  gi check src/test/java",
		Run:     func(_ *cobra.Command, args []string) { *code = runCheck(args) },
	}
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
	cmd.Flags().IntVar(&opt.jobs, "jobs", 0, "Features to run concurrently (default: CPU count, or 1 for one # gi-spec file)")
	return cmd
}

type loadCommandOptions struct {
	commonOptions
	loadOptions
	thresholds                                      []string
	part, snapshot, startAt, metrics, metricsListen string
	profile                                         string
}

func newLoadCommand(code *int) *cobra.Command {
	opt := loadCommandOptions{}
	cmd := &cobra.Command{
		Use: "load [files or directories]", Short: "Run load tests",
		Example: "  gi load users.feature --vus 10 --duration 30s\n  gi load users.feature --rate 100 --threshold 'p95 < 200ms abort'\n" +
			"  gi load users.feature --profile spike\n" +
			"  gi load users.feature --vus 400 --part 2/4 --snapshot part2.json\n  gi load merge part*.json --report load.html\n" +
			"  gi load plan users.feature --parts 4",
		Run: func(_ *cobra.Command, args []string) { *code = runLoad(args, opt) },
	}
	cmd.AddCommand(newMergeCommand(code), newPlanCommand(code))
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
	fs.StringVar(&opt.part, "part", "", "Run share i of n of the profile, e.g. 2/4")
	fs.StringVar(&opt.snapshot, "snapshot", "", "Write histograms for gi load merge")
	fs.StringVar(&opt.startAt, "start-at", "", "Wait until this RFC 3339 time before starting")
	fs.StringVar(&opt.metrics, "metrics", "", "Stream one JSON line of load metrics per second to this file")
	fs.StringVar(&opt.metricsListen, "metrics-listen", "", "Serve Prometheus text at /metrics on this address, e.g. :9090")
	fs.StringVar(&opt.profile, "profile", "", "Run one named gi-load profile, such as spike")
	return cmd
}

func newPlanCommand(code *int) *cobra.Command {
	var parts int
	var start string
	cmd := &cobra.Command{
		Use: "plan [files or directories]", Short: "Print the commands that split a load run across machines",
		Example: "  gi load plan users.feature --parts 4",
		Run:     func(_ *cobra.Command, args []string) { *code = runPlan(args, parts, start) },
	}
	cmd.Flags().IntVar(&parts, "parts", 2, "Machines that share the profile")
	cmd.Flags().StringVar(&start, "start-at", "", "RFC 3339 start time (default: one minute from now)")
	return cmd
}

func newDemoCommand(code *int) *cobra.Command {
	return &cobra.Command{
		Use: "demo", Short: "Run a greeting as a functional test and a two-second load test", Args: cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) { *code = runDemo() },
	}
}

func newInitCommand(code *int) *cobra.Command {
	return &cobra.Command{
		Use: "init", Short: "Write hello.feature and hello.mock.feature in the current directory", Args: cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) { *code = runInit() },
	}
}

func newMergeCommand(code *int) *cobra.Command {
	opt := loadCommandOptions{}
	cmd := &cobra.Command{
		Use: "merge <snapshots>", Short: "Merge load snapshots from every part and judge thresholds",
		Args: cobra.MinimumNArgs(1),
		Run:  func(_ *cobra.Command, args []string) { *code = runMerge(args, opt) },
	}
	fs := cmd.Flags()
	fs.StringVar(&opt.reportPath, "report", "", "Write a self-contained HTML report")
	fs.StringVar(&opt.junitPath, "junit", "", "Write a JUnit XML report")
	fs.StringVar(&opt.jsonPath, "json", "", "Write a JSON report")
	fs.StringArrayVar(&opt.thresholds, "threshold", nil, "Extra pass condition (repeatable)")
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
