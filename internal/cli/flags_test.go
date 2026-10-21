package cli

import (
	"flag"
	"reflect"
	"testing"
)

func TestReorderedFlagsPreserveFileTerminator(t *testing.T) {
	for _, args := range [][]string{
		{"normal.feature", "--jobs", "2", "--", "-special.feature"},
		{"--jobs=2", "--", "-special.feature", "normal.feature"},
	} {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		jobs := fs.Int("jobs", 0, "")
		if err := fs.Parse(reorderFlags(args)); err != nil {
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
