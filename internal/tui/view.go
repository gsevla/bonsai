package tui

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"bonsai/internal/format"
	"bonsai/internal/scan"
)

var (
	plainStyle  = lipgloss.NewStyle()
	boldStyle   = lipgloss.NewStyle().Bold(true)
	faintStyle  = lipgloss.NewStyle().Faint(true)
	greenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	redStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3)
)

func (m model) View() tea.View {
	content := ""
	switch {
	case m.width == 0:
	case m.mode == modeConfirm || m.mode == modeRemoving:
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, boxStyle.Render(m.confirmView()))
	default:
		content = m.listView()
	}
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

func (m model) listView() string {
	var b strings.Builder
	title := fmt.Sprintf(" bonsai v%s ── %s ", m.opts.Version, shorten(m.opts.Root))
	if utf8.RuneCountInString(title) > m.width {
		title = fit(title, max(m.width, 1))
	}
	b.WriteString(boldStyle.Render(title))
	b.WriteString(faintStyle.Render(strings.Repeat("─", max(m.width-utf8.RuneCountInString(title), 0))))
	b.WriteString("\n " + m.summary() + "\n\n")

	repoWidth := 10
	for _, w := range m.rows {
		repoWidth = max(repoWidth, utf8.RuneCountInString(w.Repo))
	}
	repoWidth = min(repoWidth, 22)
	const diskWidth, modifiedWidth, statusWidth = 9, 13, 16
	nameWidth := max(m.width-(7+repoWidth+2+2+diskWidth+2+modifiedWidth+2+statusWidth), 12)
	rule := faintStyle.Render(" " + strings.Repeat("─", max(m.width-2, 0)))

	header := " " + fit("SEL", 6) + fit("REPOSITORY", repoWidth) + "  " + fit("WORKTREE / BRANCH", nameWidth) + "  " +
		fmt.Sprintf("%*s", diskWidth, "DISK") + "  " + fit("MODIFIED", modifiedWidth) + "  " + "STATUS"
	b.WriteString(faintStyle.Render(header) + "\n" + rule + "\n")

	now := time.Now()
	height := m.listHeight()
	for line := range height {
		index := m.offset + line
		if index < len(m.rows) {
			w := m.rows[index]
			style := plainStyle
			switch {
			case index == m.cursor:
				style = boldStyle
			case w.Protected():
				style = faintStyle
			}
			mark, pointer := "[ ]", " "
			switch {
			case w.Protected():
				mark = " - "
			case m.selected[w]:
				mark = "[x]"
			}
			if index == m.cursor {
				pointer = "❯"
			}
			disk := "…"
			if w.Measured {
				disk = format.Size(w.Size)
			}
			status := w.Status()
			b.WriteString(style.Render(" " + mark + " " + pointer + " " + fit(w.Repo, repoWidth) + "  " + fit(w.Name(), nameWidth) + "  " +
				fmt.Sprintf("%*s", diskWidth, disk) + "  " + fit(format.Ago(w.Modified, now), modifiedWidth) + "  "))
			b.WriteString(statusStyle(w, style).Render(status))
		} else if line == 0 && m.mode != modeScanning {
			b.WriteString(faintStyle.Render("   No worktrees found."))
		}
		b.WriteString("\n")
	}
	b.WriteString(rule + "\n " + m.footer() + "\n " + m.help())
	return b.String()
}

func (m model) summary() string {
	if m.err != nil {
		return redStyle.Render(m.err.Error())
	}
	if m.mode == modeScanning {
		if !m.opts.NoFetch {
			return "Fetching remotes and scanning for repositories…"
		}
		return "Scanning for repositories…"
	}
	repos := map[string]bool{}
	var reclaimable int64
	for _, w := range m.all {
		repos[w.RepoDir] = true
		if !w.Protected() {
			reclaimable += w.Size
		}
	}
	noun := "repositories"
	if len(repos) == 1 {
		noun = "repository"
	}
	text := fmt.Sprintf("Found %s across %d %s · Reclaimable: %s", count(len(m.all)), len(repos), noun, format.Size(reclaimable))
	if m.pending > 0 {
		text += faintStyle.Render(fmt.Sprintf(" · measuring %d/%d", len(m.all)-m.pending, len(m.all)))
	}
	if stale := m.staleRepos(); stale > 0 {
		text += yellowStyle.Render(fmt.Sprintf(" · fetch failed for %d, status may be stale", stale))
	}
	return text
}

func (m model) staleRepos() int {
	repos := map[string]bool{}
	for _, w := range m.all {
		if w.Stale {
			repos[w.RepoDir] = true
		}
	}
	return len(repos)
}

func (m model) footer() string {
	if m.notice != "" {
		return yellowStyle.Render(m.notice)
	}
	var clean, dirty int64
	targets := m.targets()
	for _, w := range targets {
		if w.Dirty {
			dirty += w.Size
		} else {
			clean += w.Size
		}
	}
	text := fmt.Sprintf("Selected: %s (%s clean, %s dirty)", count(len(targets)), format.Size(clean), format.Size(dirty))
	if m.query != "" && m.mode != modeFilter {
		text += faintStyle.Render(" · filter: " + m.query)
	}
	return text
}

func (m model) help() string {
	if m.mode == modeFilter {
		return "Filter: " + m.query + "█" + faintStyle.Render("  [enter] keep  [esc] clear")
	}
	return faintStyle.Render(fmt.Sprintf("[space] toggle  [a] all  [m] merged  [d] delete (%d)  [/] filter  [o] sort: %s  [q] quit",
		len(m.targets()), sortNames[m.sort]))
}

func (m model) confirmView() string {
	targets := m.targets()
	var size int64
	dirty := false
	for _, w := range targets {
		size += w.Size
		dirty = dirty || w.Dirty
	}
	lines := []string{
		boldStyle.Render("Confirm removal"),
		"",
		fmt.Sprintf("You are about to remove %s to reclaim %s:", count(len(targets)), format.Size(size)),
		"",
	}
	shown := min(len(targets), max(m.height-16, 3))
	for _, w := range targets[:shown] {
		item := fmt.Sprintf("%s: %s (%s)", w.Repo, w.Name(), format.Size(w.Size))
		if w.Dirty {
			lines = append(lines, yellowStyle.Render("  ⚠ "+item+" [UNCOMMITTED CHANGES]"))
		} else {
			lines = append(lines, "  • "+item)
		}
	}
	if shown < len(targets) {
		lines = append(lines, faintStyle.Render(fmt.Sprintf("  … and %d more", len(targets)-shown)))
	}
	lines = append(lines, "", faintStyle.Render("Branches are kept, only the worktree directories go away."))
	if dirty {
		lines = append(lines, redStyle.Render("Uncommitted changes will be PERMANENTLY lost."))
	}
	if m.opts.DryRun {
		lines = append(lines, greenStyle.Render("Dry run: nothing will be removed."))
	}
	lines = append(lines, "")
	switch {
	case m.mode == modeRemoving:
		lines = append(lines, "Removing…")
	case m.mustType():
		lines = append(lines, "Type DELETE and press enter to confirm, or esc to abort:", "", "> "+m.input+"█")
	default:
		lines = append(lines, "Press y to confirm, or esc to abort.")
	}
	return strings.Join(lines, "\n")
}

func statusStyle(w *scan.Worktree, base lipgloss.Style) lipgloss.Style {
	switch {
	case w.Protected():
		return base
	case w.Dirty:
		return base.Foreground(yellowStyle.GetForeground())
	case w.Prunable:
		return base.Foreground(redStyle.GetForeground())
	case w.Merged || w.Gone:
		return base.Foreground(greenStyle.GetForeground())
	}
	return base
}

func fit(text string, width int) string {
	length := utf8.RuneCountInString(text)
	if length > width {
		return string([]rune(text)[:width-1]) + "…"
	}
	return text + strings.Repeat(" ", width-length)
}

func shorten(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
