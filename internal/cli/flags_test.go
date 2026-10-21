package cli

import (
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gi-runner/gi/internal/testutil"
	"github.com/spf13/pflag"
)

func TestFlagsPreserveFileTerminator(t *testing.T) {
	for _, args := range [][]string{
		{"normal.feature", "--jobs", "2", "--", "-special.feature"},
		{"--jobs=2", "--", "-special.feature", "normal.feature"},
	} {
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		jobs := fs.Int("jobs", 0, "")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		if *jobs != 2 {
			t.Fatalf("jobs = %d", *jobs)
		}
		want := []string{"normal.feature", "-special.feature"}
		if args[0] == "--jobs=2" {
			want = []string{"-special.feature", "normal.feature"}
		}
		if !reflect.DeepEqual(fs.Args(), want) {
			t.Fatalf("paths = %v, want %v", fs.Args(), want)
		}
	}
}

func TestDocumentedFlagsMatchCommands(t *testing.T) {
	reference := testutil.ReadMarkdown(t, filepath.Join("..", "..", "docs", "cli.md"))
	code := 0
	root := newCommand(&code)
	for _, name := range []string{"test", "load", "mock"} {
		t.Run(name, func(t *testing.T) {
			_, text, ok := strings.Cut(reference, "## "+name+"\n")
			if !ok {
				t.Fatalf("missing %s docs", name)
			}
			text, _, _ = strings.Cut(text, "\n## ")
			// Load inherits the common test flags except jobs.
			if name == "load" {
				_, common, _ := strings.Cut(reference, "## test\n")
				common, _, _ = strings.Cut(common, "\n## ")
				text += strings.ReplaceAll(common, "`--jobs n`", "")
			}
			cmd, _, err := root.Find([]string{name})
			if err != nil {
				t.Fatal(err)
			}
			documented := map[string]bool{}
			for _, match := range regexp.MustCompile("(?m)^\\| `--([a-z-]+)(?:[ `])").FindAllStringSubmatch(text, -1) {
				flagName := match[1]
				if cmd.Flags().Lookup(flagName) == nil {
					t.Errorf("docs advertise unknown flag --%s", flagName)
				}
				documented[flagName] = true
			}
			cmd.Flags().VisitAll(func(flag *pflag.Flag) {
				if !documented[flag.Name] {
					t.Errorf("--%s missing from %s docs", flag.Name, name)
				}
			})
		})
	}
}
