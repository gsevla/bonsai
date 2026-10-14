package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"text/tabwriter"
	"time"

	"bonsai/internal/format"
	"bonsai/internal/scan"
	"bonsai/internal/tui"
)

var version = "0.1.0"

const usage = `bonsai finds git worktrees under a directory and lets you prune them.

Usage:
  bonsai [OPTIONS] [PATH]

Options:
  -p, --path <DIR>       Root directory to scan (default ".")
  -d, --depth <N>        Maximum directory depth when looking for repositories (default 5)
  -a, --min-age <DAYS>   Only show worktrees unchanged for at least this many days
  -s, --min-size <SIZE>  Only show worktrees at least this large (500M, 2G)
  -n, --dry-run          Go through the removal flow without removing anything
  -f, --force            Do not require typing DELETE to remove dirty worktrees
      --no-fetch         Do not fetch remotes before scanning (status may be stale)
      --plain            Print a plain table instead of opening the TUI
      --json             Print worktree metadata as JSON
  -h, --help             Print this help
  -v, --version          Print the version
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bonsai:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var path, minSize string
	var depth, minAge int
	var dryRun, force, plain, asJSON, showVersion, noFetch bool

	flags := flag.NewFlagSet("bonsai", flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprint(flags.Output(), usage) }
	for _, name := range []string{"p", "path"} {
		flags.StringVar(&path, name, ".", "")
	}
	for _, name := range []string{"d", "depth"} {
		flags.IntVar(&depth, name, 5, "")
	}
	for _, name := range []string{"a", "min-age"} {
		flags.IntVar(&minAge, name, 0, "")
	}
	for _, name := range []string{"s", "min-size"} {
		flags.StringVar(&minSize, name, "0", "")
	}
	for _, name := range []string{"n", "dry-run"} {
		flags.BoolVar(&dryRun, name, false, "")
	}
	for _, name := range []string{"f", "force"} {
		flags.BoolVar(&force, name, false, "")
	}
	for _, name := range []string{"v", "version"} {
		flags.BoolVar(&showVersion, name, false, "")
	}
	flags.BoolVar(&noFetch, "no-fetch", false, "")
	flags.BoolVar(&plain, "plain", false, "")
	flags.BoolVar(&asJSON, "json", false, "")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		os.Exit(2)
	}
	if showVersion {
		fmt.Println("bonsai", version)
		return nil
	}
	if flags.NArg() > 1 {
		return errors.New("expected at most one PATH, options go before it")
	}
	if flags.NArg() == 1 {
		path = flags.Arg(0)
	}
	size, err := format.ParseSize(minSize)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git was not found in PATH")
	}
	filter := scan.Filter{MinAge: time.Duration(minAge) * 24 * time.Hour, MinSize: size}

	if !plain && !asJSON {
		return tui.Run(tui.Options{
			Root:    path,
			Depth:   depth,
			Filter:  filter,
			DryRun:  dryRun,
			Force:   force,
			NoFetch: noFetch,
			Version: version,
		})
	}

	found, err := scan.Discover(path, depth, !noFetch)
	if err != nil {
		return err
	}
	scan.MeasureAll(found)
	stale := map[string]bool{}
	for _, w := range found {
		if w.Stale {
			stale[w.RepoDir] = true
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(os.Stderr, "bonsai: fetch failed for %d repositories, status may be stale\n", len(stale))
	}
	now := time.Now()
	list := []*scan.Worktree{}
	for _, w := range found {
		if filter.Match(w, now) {
			list = append(list, w)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Repo < list[j].Repo })

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(list)
	}
	table := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "REPOSITORY\tWORKTREE\tDISK\tMODIFIED\tSTATUS\tPATH")
	for _, w := range list {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n", w.Repo, w.Name(), format.Size(w.Size), format.Ago(w.Modified, now), w.Status(), w.Path)
	}
	return table.Flush()
}
