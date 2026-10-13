package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Worktree struct {
	Repo     string    `json:"repository"`
	RepoDir  string    `json:"repository_dir"`
	Path     string    `json:"path"`
	Branch   string    `json:"branch,omitempty"`
	Head     string    `json:"head,omitempty"`
	Size     int64     `json:"size_bytes"`
	Modified time.Time `json:"modified,omitzero"`
	Main     bool      `json:"main"`
	Bare     bool      `json:"bare"`
	Base     bool      `json:"base"`
	Detached bool      `json:"detached"`
	Locked   bool      `json:"locked"`
	Prunable bool      `json:"prunable"`
	Merged   bool      `json:"merged"`
	Gone     bool      `json:"upstream_gone"`
	Ahead    int       `json:"ahead"`
	Dirty    bool      `json:"dirty"`
	Measured bool      `json:"-"`
}

type Measurement struct {
	Size     int64
	Modified time.Time
	Dirty    bool
}

type Filter struct {
	MinAge  time.Duration
	MinSize int64
}

func (f Filter) Match(w *Worktree, now time.Time) bool {
	if w.Size < f.MinSize {
		return false
	}
	return f.MinAge == 0 || now.Sub(w.Modified) >= f.MinAge
}

func (w *Worktree) Protected() bool {
	return w.Main || w.Base || w.Locked
}

func (w *Worktree) Name() string {
	switch {
	case w.Bare:
		return "(bare)"
	case w.Branch != "":
		return w.Branch
	case w.Detached && len(w.Head) >= 7:
		return "(detached " + w.Head[:7] + ")"
	}
	return filepath.Base(w.Path)
}

func (w *Worktree) Status() string {
	var parts []string
	switch {
	case w.Locked:
		parts = append(parts, "locked")
	case w.Protected():
		parts = append(parts, "protected")
	case w.Prunable:
		parts = append(parts, "missing")
	case w.Merged:
		parts = append(parts, "merged")
	case w.Gone:
		parts = append(parts, "gone")
	case w.Ahead > 0:
		parts = append(parts, fmt.Sprintf("%d ahead", w.Ahead))
	}
	if w.Dirty {
		parts = append(parts, "dirty")
	}
	if len(parts) == 0 {
		return "clean"
	}
	return strings.Join(parts, ", ")
}

func (w *Worktree) Apply(m Measurement) {
	w.Size = m.Size
	w.Dirty = m.Dirty
	if m.Modified.After(w.Modified) {
		w.Modified = m.Modified
	}
	w.Measured = true
}

func Discover(root string, depth int) ([]*Worktree, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	seen := map[string]bool{}
	var repos []string
	for _, dir := range candidates(root, depth) {
		out, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			continue
		}
		common := filepath.Clean(strings.TrimSpace(out))
		if !seen[common] {
			seen[common] = true
			repos = append(repos, common)
		}
	}
	results := make([][]*Worktree, len(repos))
	var wg sync.WaitGroup
	for i, common := range repos {
		wg.Go(func() { results[i] = loadRepo(common) })
	}
	wg.Wait()
	var all []*Worktree
	for _, list := range results {
		all = append(all, list...)
	}
	return all, nil
}

func Measure(w *Worktree) Measurement {
	var m Measurement
	if w.Prunable {
		return m
	}
	m.Size, m.Modified = usage(w.Path)
	if !w.Bare {
		out, err := git(w.Path, "status", "--porcelain")
		m.Dirty = err == nil && strings.TrimSpace(out) != ""
	}
	return m
}

func MeasureAll(list []*Worktree) {
	results := make([]Measurement, len(list))
	limit := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, w := range list {
		wg.Go(func() {
			limit <- struct{}{}
			results[i] = Measure(w)
			<-limit
		})
	}
	wg.Wait()
	for i, w := range list {
		w.Apply(results[i])
	}
}

func Remove(w *Worktree) error {
	args := []string{"worktree", "remove"}
	if w.Dirty {
		args = append(args, "--force")
	}
	_, err := git(w.RepoDir, append(args, w.Path)...)
	return err
}

func candidates(root string, depth int) []string {
	var found []string
	var walk func(dir string, level int)
	walk = func(dir string, level int) {
		if isRepo(dir) {
			found = append(found, dir)
			return
		}
		if level >= depth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "node_modules" {
				walk(filepath.Join(dir, entry.Name()), level+1)
			}
		}
	}
	walk(root, 0)
	return found
}

func isRepo(dir string) bool {
	if exists(filepath.Join(dir, ".git")) {
		return true
	}
	return exists(filepath.Join(dir, "HEAD")) && exists(filepath.Join(dir, "objects")) && exists(filepath.Join(dir, "refs"))
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func repoName(common string) string {
	name := filepath.Base(common)
	if strings.HasPrefix(name, ".") {
		return filepath.Base(filepath.Dir(common))
	}
	return strings.TrimSuffix(name, ".git")
}

func loadRepo(common string) []*Worktree {
	out, err := git(common, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil
	}
	name := repoName(common)
	var list []*Worktree
	for record := range strings.SplitSeq(out, "\x00\x00") {
		w := &Worktree{Repo: name, RepoDir: common, Main: len(list) == 0}
		for field := range strings.SplitSeq(record, "\x00") {
			key, value, _ := strings.Cut(field, " ")
			switch key {
			case "worktree":
				w.Path = value
			case "HEAD":
				w.Head = value
			case "branch":
				w.Branch = strings.TrimPrefix(value, "refs/heads/")
			case "bare":
				w.Bare = true
			case "detached":
				w.Detached = true
			case "locked":
				w.Locked = true
			case "prunable":
				w.Prunable = true
			}
		}
		if w.Path != "" {
			list = append(list, w)
		}
	}
	markMerged(common, list)
	markCommitted(common, list)
	return list
}

func baseRefs(common string) (string, []string) {
	var names []string
	if out, err := git(common, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		names = append(names, strings.TrimPrefix(strings.TrimSpace(out), "origin/"))
	}
	names = append(names, "main", "master")
	if out, err := git(common, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		names = append(names, strings.TrimSpace(out))
	}
	for _, name := range names {
		var refs []string
		for _, ref := range []string{"refs/remotes/origin/" + name, "refs/heads/" + name} {
			if _, err := git(common, "rev-parse", "--verify", "--quiet", ref); err == nil {
				refs = append(refs, ref)
			}
		}
		if len(refs) > 0 {
			return name, refs
		}
	}
	return "", nil
}

func markMerged(common string, list []*Worktree) {
	base, refs := baseRefs(common)
	format := "%(refname:short)%00%(upstream:track)"
	for _, ref := range refs {
		format += "%00%(ahead-behind:" + ref + ")"
	}
	out, err := git(common, "for-each-ref", "--format="+format, "refs/heads")
	if err != nil {
		return
	}
	type state struct {
		gone  bool
		ahead int
	}
	branches := map[string]state{}
	for line := range strings.Lines(out) {
		fields := strings.Split(strings.TrimRight(line, "\n"), "\x00")
		if len(fields) < 2 {
			continue
		}
		s := state{gone: strings.Contains(fields[1], "gone"), ahead: -1}
		for _, counts := range fields[2:] {
			first, _, _ := strings.Cut(counts, " ")
			if n, err := strconv.Atoi(first); err == nil && (s.ahead < 0 || n < s.ahead) {
				s.ahead = n
			}
		}
		branches[fields[0]] = s
	}
	for _, w := range list {
		s, ok := branches[w.Branch]
		if !ok || w.Branch == "" {
			continue
		}
		w.Base = w.Branch == base
		w.Gone = s.gone
		w.Merged = s.ahead == 0
		w.Ahead = max(s.ahead, 0)
	}
}

func markCommitted(common string, list []*Worktree) {
	args := []string{"log", "--no-walk=unsorted", "--format=%H %ct"}
	for _, w := range list {
		if w.Head != "" && strings.Trim(w.Head, "0") != "" {
			args = append(args, w.Head)
		}
	}
	if len(args) == 3 {
		return
	}
	out, err := git(common, args...)
	if err != nil {
		return
	}
	times := map[string]time.Time{}
	for line := range strings.Lines(out) {
		sha, stamp, _ := strings.Cut(strings.TrimSpace(line), " ")
		if seconds, err := strconv.ParseInt(stamp, 10, 64); err == nil {
			times[sha] = time.Unix(seconds, 0)
		}
	}
	for _, w := range list {
		w.Modified = times[w.Head]
	}
}
