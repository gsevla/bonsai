package tui

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"bonsai/internal/format"
	"bonsai/internal/scan"
)

type Options struct {
	Root    string
	Depth   int
	Filter  scan.Filter
	DryRun  bool
	Force   bool
	Version string
}

type mode int

const (
	modeScanning mode = iota
	modeList
	modeFilter
	modeConfirm
	modeRemoving
)

type sortKey int

const (
	sortRepo sortKey = iota
	sortSize
	sortModified
)

var sortNames = []string{"repo", "size", "modified"}

type discoveredMsg struct {
	list []*scan.Worktree
	err  error
}

type measuredMsg struct {
	worktree    *scan.Worktree
	measurement scan.Measurement
}

type removedMsg struct {
	removed []*scan.Worktree
	failed  []string
}

type model struct {
	opts     Options
	mode     mode
	all      []*scan.Worktree
	rows     []*scan.Worktree
	selected map[*scan.Worktree]bool
	cursor   int
	offset   int
	sort     sortKey
	query    string
	input    string
	pending  int
	notice   string
	err      error
	width    int
	height   int
	limit    chan struct{}
}

func Run(opts Options) error {
	if root, err := filepath.Abs(opts.Root); err == nil {
		opts.Root = root
	}
	m := model{
		opts:     opts,
		selected: map[*scan.Worktree]bool{},
		limit:    make(chan struct{}, runtime.NumCPU()),
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		list, err := scan.Discover(m.opts.Root, m.opts.Depth)
		return discoveredMsg{list, err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scroll()
	case discoveredMsg:
		m.mode = modeList
		m.err = msg.err
		m.all = msg.list
		m.pending = len(msg.list)
		m.refresh()
		cmds := make([]tea.Cmd, len(msg.list))
		for i, w := range msg.list {
			cmds[i] = m.measure(w)
		}
		return m, tea.Batch(cmds...)
	case measuredMsg:
		msg.worktree.Apply(msg.measurement)
		m.pending--
		m.refresh()
	case removedMsg:
		m.finishRemoval(msg)
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m model) measure(w *scan.Worktree) tea.Cmd {
	return func() tea.Msg {
		m.limit <- struct{}{}
		defer func() { <-m.limit }()
		return measuredMsg{w, scan.Measure(w)}
	}
}

func (m model) remove(targets []*scan.Worktree) tea.Cmd {
	dryRun := m.opts.DryRun
	return func() tea.Msg {
		var result removedMsg
		for _, w := range targets {
			if !dryRun {
				if err := scan.Remove(w); err != nil {
					result.failed = append(result.failed, fmt.Sprintf("%s: %s: %v", w.Repo, w.Name(), err))
					continue
				}
			}
			result.removed = append(result.removed, w)
		}
		return result
	}
}

func (m *model) finishRemoval(msg removedMsg) {
	var size int64
	gone := map[*scan.Worktree]bool{}
	for _, w := range msg.removed {
		size += w.Size
		gone[w] = true
	}
	m.mode = modeList
	if m.opts.DryRun {
		m.notice = fmt.Sprintf("Dry run: would remove %s (%s)", count(len(msg.removed)), format.Size(size))
		return
	}
	kept := m.all[:0]
	for _, w := range m.all {
		if gone[w] {
			delete(m.selected, w)
		} else {
			kept = append(kept, w)
		}
	}
	m.all = kept
	m.notice = fmt.Sprintf("Removed %s, reclaimed %s", count(len(msg.removed)), format.Size(size))
	if len(msg.failed) > 0 {
		m.notice += fmt.Sprintf(" · %d failed: %s", len(msg.failed), msg.failed[0])
	}
	m.refresh()
}

func (m model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeScanning:
		if key == "q" || key == "esc" {
			return m, tea.Quit
		}
	case modeFilter:
		switch key {
		case "enter":
			m.mode = modeList
		case "esc":
			m.mode = modeList
			m.query = ""
		case "backspace":
			m.query = dropLast(m.query)
		default:
			m.query += msg.Text
		}
		m.refresh()
	case modeConfirm:
		return m.confirmKey(key, msg.Text)
	case modeList:
		return m.listKey(key)
	}
	return m, nil
}

func (m model) confirmKey(key, text string) (tea.Model, tea.Cmd) {
	confirmed := false
	switch {
	case key == "esc":
		m.mode = modeList
	case !m.mustType():
		switch key {
		case "y", "enter":
			confirmed = true
		case "n", "q":
			m.mode = modeList
		}
	case key == "enter":
		confirmed = m.input == "DELETE"
	case key == "backspace":
		m.input = dropLast(m.input)
	default:
		m.input += text
	}
	if confirmed {
		m.mode = modeRemoving
		return m, m.remove(m.targets())
	}
	return m, nil
}

func (m model) listKey(key string) (tea.Model, tea.Cmd) {
	m.notice = ""
	page := m.listHeight()
	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		if m.query == "" {
			return m, tea.Quit
		}
		m.query = ""
		m.refresh()
	case "up", "k":
		m.cursor--
	case "down", "j":
		m.cursor++
	case "pgup":
		m.cursor -= page
	case "pgdown":
		m.cursor += page
	case "home", "g":
		m.cursor = 0
	case "end", "G", "shift+g":
		m.cursor = len(m.rows) - 1
	case "space":
		if w := m.current(); w != nil && !w.Protected() {
			m.selected[w] = !m.selected[w]
		}
	case "a":
		m.toggleAll()
	case "m":
		for _, w := range m.rows {
			if w.Measured && w.Merged && !w.Dirty && !w.Protected() {
				m.selected[w] = true
			}
		}
	case "o":
		m.sort = (m.sort + 1) % sortKey(len(sortNames))
		m.refresh()
	case "/":
		m.mode = modeFilter
	case "d":
		if len(m.targets()) == 0 {
			m.notice = "Nothing selected"
		} else {
			m.mode = modeConfirm
			m.input = ""
		}
	}
	m.scroll()
	return m, nil
}

func (m *model) toggleAll() {
	all := true
	for _, w := range m.rows {
		if !w.Protected() && !m.selected[w] {
			all = false
		}
	}
	for _, w := range m.rows {
		if !w.Protected() {
			m.selected[w] = !all
		}
	}
}

func (m model) current() *scan.Worktree {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor]
}

func (m model) targets() []*scan.Worktree {
	var list []*scan.Worktree
	for _, w := range m.rows {
		if m.selected[w] && !w.Protected() {
			list = append(list, w)
		}
	}
	return list
}

func (m model) mustType() bool {
	if m.opts.Force {
		return false
	}
	for _, w := range m.targets() {
		if w.Dirty {
			return true
		}
	}
	return false
}

func (m model) listHeight() int {
	return max(m.height-8, 1)
}

func (m *model) refresh() {
	focused := m.current()
	now := time.Now()
	query := strings.ToLower(m.query)
	m.rows = m.rows[:0:0]
	for _, w := range m.all {
		if w.Measured && !m.opts.Filter.Match(w, now) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(w.Repo+" "+w.Name()), query) {
			continue
		}
		m.rows = append(m.rows, w)
	}
	sort.SliceStable(m.rows, func(i, j int) bool {
		a, b := m.rows[i], m.rows[j]
		switch m.sort {
		case sortSize:
			return a.Size > b.Size
		case sortModified:
			if a.Measured != b.Measured {
				return a.Measured
			}
			if !a.Measured {
				return false
			}
			return a.Modified.Before(b.Modified)
		}
		return a.Repo < b.Repo
	})
	for i, w := range m.rows {
		if w == focused {
			m.cursor = i
		}
	}
	m.scroll()
}

func (m *model) scroll() {
	m.cursor = max(min(m.cursor, len(m.rows)-1), 0)
	height := m.listHeight()
	m.offset = min(m.offset, m.cursor)
	m.offset = max(m.offset, m.cursor-height+1)
	m.offset = max(min(m.offset, len(m.rows)-height), 0)
}

func dropLast(text string) string {
	_, size := utf8.DecodeLastRuneInString(text)
	return text[:len(text)-size]
}

func count(n int) string {
	if n == 1 {
		return "1 worktree"
	}
	return fmt.Sprintf("%d worktrees", n)
}
