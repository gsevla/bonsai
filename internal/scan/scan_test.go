package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=bonsai", "-c", "user.email=bonsai@example.com"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) (string, map[string]*Worktree) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "projects", "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "init", "-b", "main")
	write(t, filepath.Join(repo, "readme"))
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-m", "first")

	merged := filepath.Join(root, "projects", "app-merged")
	run(t, repo, "worktree", "add", "-b", "feat/merged", merged)

	ahead := filepath.Join(root, "projects", "app-ahead")
	run(t, repo, "worktree", "add", "-b", "feat/ahead", ahead)
	write(t, filepath.Join(ahead, "feature"))
	run(t, ahead, "add", ".")
	run(t, ahead, "commit", "-m", "second")

	dirty := filepath.Join(root, "projects", "app-dirty")
	run(t, repo, "worktree", "add", "-b", "feat/dirty", dirty)
	write(t, filepath.Join(dirty, "untracked"))

	list, err := Discover(root, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	MeasureAll(list)
	byName := map[string]*Worktree{}
	for _, w := range list {
		byName[w.Name()] = w
	}
	if len(byName) != 4 {
		t.Fatalf("expected 4 worktrees, got %d", len(list))
	}
	return root, byName
}

func TestDiscover(t *testing.T) {
	_, found := fixture(t)

	main := found["main"]
	if !main.Protected() || main.Repo != "app" || main.Size == 0 || main.Modified.IsZero() {
		t.Errorf("unexpected main worktree: %+v", main)
	}
	if w := found["feat/merged"]; !w.Merged || w.Dirty || w.Protected() || w.Status() != "merged" {
		t.Errorf("unexpected merged worktree: %+v", w)
	}
	if w := found["feat/ahead"]; w.Merged || w.Ahead != 1 || w.Status() != "1 ahead" {
		t.Errorf("unexpected ahead worktree: %+v", w)
	}
	if w := found["feat/dirty"]; !w.Dirty || w.Status() != "merged, dirty" {
		t.Errorf("unexpected dirty worktree: %+v", w)
	}
}

func TestDepthLimit(t *testing.T) {
	root, _ := fixture(t)
	list, err := Discover(root, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("expected nothing within depth 1, got %d", len(list))
	}
}

func TestRemove(t *testing.T) {
	root, found := fixture(t)
	for _, name := range []string{"feat/merged", "feat/dirty"} {
		if err := Remove(found[name]); err != nil {
			t.Fatalf("remove %s: %v", name, err)
		}
		if exists(found[name].Path) {
			t.Errorf("%s still exists", name)
		}
	}
	missing := found["feat/ahead"]
	if err := os.RemoveAll(missing.Path); err != nil {
		t.Fatal(err)
	}
	if err := Remove(missing); err != nil {
		t.Fatalf("remove missing worktree: %v", err)
	}
	list, err := Discover(root, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].Main {
		t.Errorf("expected only the main worktree to remain, got %d", len(list))
	}
}

func TestBareLayout(t *testing.T) {
	root, _ := fixture(t)
	hub := filepath.Join(root, "hub")
	if err := os.MkdirAll(hub, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, hub, "clone", "--bare", filepath.Join(root, "projects", "app"), ".bare")
	run(t, hub, "--git-dir=.bare", "worktree", "add", filepath.Join(hub, "main"), "main")
	run(t, hub, "--git-dir=.bare", "worktree", "add", filepath.Join(hub, "ahead"), "feat/ahead")

	list, err := Discover(hub, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]*Worktree{}
	for _, w := range list {
		found[w.Name()] = w
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(list))
	}
	if w := found["(bare)"]; w == nil || !w.Protected() || w.Repo != "hub" {
		t.Errorf("unexpected bare entry: %+v", w)
	}
	if w := found["main"]; w == nil || !w.Protected() {
		t.Errorf("base branch worktree should be protected: %+v", w)
	}
	if w := found["feat/ahead"]; w == nil || w.Protected() || w.Merged {
		t.Errorf("unexpected linked worktree: %+v", w)
	}
}

func TestFetchUpdatesRemoteRefs(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(root, "origin")
	clone := filepath.Join(root, "work", "clone")
	for _, dir := range []string{origin, filepath.Dir(clone)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run(t, origin, "init", "-b", "main")
	write(t, filepath.Join(origin, "readme"))
	run(t, origin, "add", ".")
	run(t, origin, "commit", "-m", "first")
	run(t, filepath.Dir(clone), "clone", origin, clone)
	run(t, origin, "checkout", "-b", "other")
	run(t, origin, "checkout", "main")

	for _, fetch := range []bool{false, true} {
		list, err := Discover(filepath.Dir(clone), 3, fetch)
		if err != nil || len(list) != 1 {
			t.Fatalf("discover: %v %d", err, len(list))
		}
		if list[0].Stale {
			t.Fatalf("fetch=%v: unexpected stale", fetch)
		}
		out, _ := git(clone, "branch", "-r", "--list", "origin/other")
		if has := out != ""; has != fetch {
			t.Fatalf("fetch=%v: origin/other present=%v", fetch, has)
		}
	}
}

func TestFetchFailureMarksStale(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "init", "-b", "main")
	write(t, filepath.Join(repo, "readme"))
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-m", "first")
	run(t, repo, "remote", "add", "origin", filepath.Join(root, "missing"))

	for _, fetch := range []bool{false, true} {
		list, err := Discover(root, 3, fetch)
		if err != nil || len(list) != 1 {
			t.Fatalf("discover: %v %d", err, len(list))
		}
		if list[0].Stale != fetch {
			t.Fatalf("fetch=%v: stale=%v", fetch, list[0].Stale)
		}
	}
}

func TestSquashMerged(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "app")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "init", "-b", "main")
	write(t, filepath.Join(repo, "readme"))
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-m", "first")

	feature := filepath.Join(root, "app-feature")
	run(t, repo, "worktree", "add", "-b", "feat/squash", feature)
	write(t, filepath.Join(feature, "one"))
	run(t, feature, "add", ".")
	run(t, feature, "commit", "-m", "one")
	write(t, filepath.Join(feature, "two"))
	run(t, feature, "add", ".")
	run(t, feature, "commit", "-m", "two")

	run(t, repo, "merge", "--squash", "feat/squash")
	run(t, repo, "commit", "-m", "squash feat")
	write(t, filepath.Join(repo, "later"))
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-m", "later")

	list, err := Discover(root, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range list {
		if w.Branch != "feat/squash" {
			continue
		}
		if !w.Merged || w.Ahead != 2 || w.Status() != "merged" {
			t.Errorf("expected squash-merged worktree, got %+v", w)
		}
		return
	}
	t.Fatal("feat/squash worktree not found")
}
