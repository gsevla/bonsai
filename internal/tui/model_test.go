package tui

import (
	"testing"
	"time"

	"bonsai/internal/scan"
)

func TestRefreshModifiedKeepsUnmeasuredRowsLast(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seeded := &scan.Worktree{Repo: "seeded", Modified: base.Add(-48 * time.Hour)}
	old := &scan.Worktree{Repo: "old", Modified: base, Measured: true}
	recent := &scan.Worktree{Repo: "recent", Modified: base.Add(24 * time.Hour), Measured: true}
	m := model{all: []*scan.Worktree{seeded, recent, old}, sort: sortModified}
	m.refresh()
	want := []*scan.Worktree{old, recent, seeded}
	for i := range want {
		if m.rows[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, m.rows[i].Repo, want[i].Repo)
		}
	}
}
