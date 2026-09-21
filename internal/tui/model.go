package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mikeoertli/github-pr-monitor/internal/config"
	"github.com/mikeoertli/github-pr-monitor/internal/core"
)

type Source interface {
	Fetch(context.Context, core.Ref) core.PR
	Discover(context.Context) ([]core.Ref, error)
}
type Actions struct {
	Clipboard func(context.Context) (string, error)
	Open      func(context.Context, string) error
	Copy      func(context.Context, string) error
}
type Model struct {
	Config                                      config.Config
	PRs                                         []core.PR
	Dismissed                                   map[string]core.Ref
	Source                                      Source
	Actions                                     Actions
	Context                                     context.Context
	StatePath                                   string
	Demo                                        bool
	Started                                     time.Time
	QuitReason                                  string
	SaveError                                   error
	width, height, cursor, jobCursor            int
	input                                       textinput.Model
	mode, filter                                string
	filterBeforeEdit                            string
	help, busy, importing, paused, refreshAgain bool
	notice                                      string
	lastTick, lastPoll, lastSave                time.Time
	demoStep                                    int
	expanded                                    map[string]bool
	scroll, detailScroll                        int
	detailFocus                                 string
}
type tickMsg time.Time
type pollMsg struct{ PRs []core.PR }
type importMsg struct {
	Refs  []core.Ref
	Err   error
	Label string
}
type openMsg struct{ Err error }
type copyMsg struct {
	Label string
	Err   error
	Count int
}

func New(ctx context.Context, c config.Config, prs []core.PR, source Source, actions Actions, state string, demo bool) *Model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 32768
	input.Width = 70
	now := time.Now()
	return &Model{Context: ctx, Config: c, PRs: prs, Source: source, Actions: actions, StatePath: state, Demo: demo, Started: now, lastTick: now, width: 120, height: 30, input: input, expanded: map[string]bool{}, Dismissed: map[string]core.Ref{}}
}

// SetFilter changes both the visible rows and the polling scope. PRs returning
// to the scope must refresh before their saved status can trigger auto-quit.
func (m *Model) SetFilter(filter string) {
	if filter == m.filter {
		return
	}
	before := map[int]bool{}
	for _, i := range m.PollingRows() {
		before[i] = true
	}
	m.account(time.Now())
	m.filter = filter
	m.cursor, m.jobCursor, m.detailScroll, m.scroll = 0, 0, 0, 0
	m.detailFocus = ""
	for _, i := range m.PollingRows() {
		if !before[i] {
			m.PRs[i].Fresh = false
		}
	}
}

// PollingRows uses the same matcher as the table, independently of discovery.
func (m *Model) PollingRows() []int {
	return core.Sorted(m.PRs, m.filter, m.Config.Sort, m.Config.Descending)
}

func (m *Model) scopedPRs() []core.PR {
	var prs []core.PR
	for _, i := range m.PollingRows() {
		prs = append(prs, m.PRs[i])
	}
	return prs
}

// SummaryPRs includes dismissed/expired matching rows without exposing PRs
// excluded by the filter. Filtering never removes them from the saved session.
func (m *Model) SummaryPRs() []core.PR {
	prs := append([]core.PR(nil), m.PRs...)
	for i := range prs {
		prs[i].Removed = false
	}
	var result []core.PR
	for _, i := range core.Sorted(prs, m.filter, m.Config.Sort, m.Config.Descending) {
		result = append(result, m.PRs[i])
	}
	return result
}

func (m *Model) refreshFilter() tea.Cmd {
	if m.paused {
		return nil
	}
	return m.poll()
}
func (m *Model) Init() tea.Cmd { return tea.Batch(tick(), m.poll()) }
func tick() tea.Cmd            { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }
func (m *Model) poll() tea.Cmd {
	if m.mode == "filter" {
		return nil
	}
	if m.busy {
		m.refreshAgain = true
		return nil
	}
	refs := []core.Ref{}
	for _, i := range m.PollingRows() {
		refs = append(refs, m.PRs[i].Ref)
	}
	if len(refs) == 0 {
		return nil
	}
	m.busy = true
	m.lastPoll = time.Now()
	if m.Demo {
		m.demoStep++
		step := m.demoStep
		return func() tea.Msg { return pollMsg{PRs: DemoSnapshots(refs, step)} }
	}
	source, ctx := m.Source, m.Context
	return func() tea.Msg {
		// Bound concurrent gh processes while keeping slow PRs off the input loop.
		result := make([]core.PR, len(refs))
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for i, ref := range refs {
			wg.Add(1)
			go func(i int, ref core.Ref) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					result[i] = core.PR{Ref: ref, Error: "poll cancelled"}
					return
				}
				defer func() { <-sem }()
				result[i] = source.Fetch(ctx, ref)
			}(i, ref)
		}
		wg.Wait()
		return pollMsg{PRs: result}
	}
}
func (m *Model) add(refs []core.Ref) int {
	m.account(time.Now())
	n := 0
	for _, ref := range refs {
		delete(m.Dismissed, strings.ToLower(ref.URL))
		found := false
		for i, p := range m.PRs {
			if strings.EqualFold(p.Ref.URL, ref.URL) {
				found = true
				if p.Removed {
					m.PRs[i].Removed = false
					m.PRs[i].Expired = false
					m.PRs[i].Fresh = false
					n++
				}
				break
			}
		}
		if !found {
			m.PRs = append(m.PRs, core.NewPR(ref, time.Now()))
			n++
		}
	}
	m.save()
	return n
}
func (m *Model) save() {
	if m.Demo || m.StatePath == "" {
		return
	}
	var dismissed []core.Ref
	for _, ref := range m.Dismissed {
		dismissed = append(dismissed, ref)
	}
	m.SaveError = core.SaveSession(m.StatePath, m.PRs, dismissed...)
	if err := m.SaveError; err != nil {
		m.notice = "Could not save session: " + core.Clean(err.Error())
	}
	m.lastSave = time.Now()
}
func (m *Model) account(now time.Time) {
	d := now.Sub(m.lastTick)
	if d < 0 {
		d = 0
	}
	for _, i := range m.PollingRows() {
		if !m.PRs[i].Removed {
			m.PRs[i].Monitored += d
			for _, j := range m.PRs[i].Jobs {
				key := j.Key + "\x00" + j.RunID
				if observed, ok := m.PRs[i].History[key]; ok {
					observed.Monitored += d
					m.PRs[i].History[key] = observed
				}
			}
		}
	}
	m.lastTick = now
}

// ExpireCompleted only removes successfully refreshed closed snapshots, so an
// offline or newly reopened PR cannot disappear based on unverified saved data.
func (m *Model) ExpireCompleted(now time.Time) {
	count := 0
	for i := range m.PRs {
		if m.PRs[i].RetentionExpired(now, m.Config.RetentionDuration()) {
			m.PRs[i].Removed, m.PRs[i].Expired = true, true
			count++
		}
	}
	if count > 0 {
		m.notice = fmt.Sprintf("%d completed PR(s) expired; retained in this session's summary.", count)
	}
}

func (m *Model) Finish() { m.account(time.Now()); m.save() }
func (m *Model) selected() int {
	rows := core.Sorted(m.PRs, m.filter, m.Config.Sort, m.Config.Descending)
	if m.detailFocus != "" {
		found := false
		for pos, i := range rows {
			if m.PRs[i].Ref.URL == m.detailFocus {
				m.cursor, found = pos, true
				break
			}
		}
		if !found {
			m.detailFocus, m.detailScroll = "", 0
		}
	}
	if len(rows) == 0 {
		return -1
	}
	m.cursor = max(0, min(m.cursor, len(rows)-1))
	return rows[m.cursor]
}
func (m *Model) startInput(mode string) tea.Cmd {
	m.detailFocus, m.detailScroll = "", 0
	m.mode = mode
	m.input.SetValue("")
	m.input.Placeholder = "PR URLs or owner/repo#123 (multiple accepted)"
	if mode == "filter" {
		m.filterBeforeEdit = m.filter
		m.input.SetValue(m.filter)
		m.input.Placeholder = "fuzzy match repository, title, status, phase…"
	}
	m.input.Focus()
	return textinput.Blink
}
func (m *Model) importClipboard() tea.Cmd {
	if m.importing {
		return nil
	}
	m.importing = true
	ctx, read, host := m.Context, m.Actions.Clipboard, m.Config.GitHubHost
	return func() tea.Msg {
		text, err := read(ctx)
		if err != nil {
			return importMsg{Err: err}
		}
		refs, err := core.ParseBatch(text, host)
		return importMsg{Refs: refs, Err: err, Label: "clipboard"}
	}
}
func (m *Model) discover() tea.Cmd {
	if m.importing {
		return nil
	}
	m.importing = true
	source, ctx := m.Source, m.Context
	if m.Demo {
		return func() tea.Msg { return importMsg{Err: fmt.Errorf("discovery is disabled in the offline demo")} }
	}
	return func() tea.Msg {
		refs, err := source.Discover(ctx)
		return importMsg{Refs: refs, Err: err, Label: "discovery"}
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(10, msg.Width-12)
	case tickMsg:
		now := time.Time(msg)
		m.account(now)
		if now.Sub(m.lastSave) >= 10*time.Second {
			m.save()
		}
		var cmd tea.Cmd
		if !m.paused && m.mode != "filter" && now.Sub(m.lastPoll) >= m.Config.PollInterval() && !m.busy {
			cmd = m.poll()
		}
		return m, tea.Batch(tick(), cmd)
	case pollMsg:
		m.account(time.Now())
		m.busy = false
		for _, next := range msg.PRs {
			for i := range m.PRs {
				if m.PRs[i].Ref.URL == next.Ref.URL && !m.PRs[i].Removed {
					m.PRs[i].Apply(next, time.Now())
					break
				}
			}
		}
		quitReady := core.ShouldQuit(m.scopedPRs(), m.Config.AutoQuit)
		m.ExpireCompleted(time.Now())
		m.save()
		if m.refreshAgain {
			m.refreshAgain = false
			return m, m.poll()
		}
		if m.mode == "" && !m.importing && !m.paused && quitReady {
			m.QuitReason = "auto-quit: " + m.Config.AutoQuit
			return m, tea.Quit
		}
	case importMsg:
		m.importing = false
		if msg.Err != nil {
			m.notice = core.Clean(msg.Err.Error())
			break
		}
		refs := msg.Refs
		if msg.Label == "discovery" {
			refs = nil
			for _, ref := range msg.Refs {
				if _, skip := m.Dismissed[strings.ToLower(ref.URL)]; !skip {
					refs = append(refs, ref)
				}
			}
		}
		n := m.add(refs)
		m.notice = fmt.Sprintf("Added %d PRs from %s (%d already monitored or dismissed).", n, msg.Label, len(msg.Refs)-n)
		if msg.Label == "discovery" && len(msg.Refs) >= m.Config.DiscoveryLimit {
			m.notice += " Discovery limit reached; increase discovery_limit for more."
		}
		return m, m.poll()
	case copyMsg:
		if msg.Err != nil {
			m.notice = core.Clean(msg.Err.Error())
		} else if msg.Label != "" {
			m.notice = "Copied " + msg.Label + "."
		} else {
			m.notice = fmt.Sprintf("Copied JSON for %d PR(s).", msg.Count)
		}
	case openMsg:
		if msg.Err != nil {
			m.notice = core.Clean(msg.Err.Error())
		}
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			m.QuitReason = "quit"
			return m, tea.Quit
		}
		if m.mode != "" {
			switch key {
			case "esc":
				filtering := m.mode == "filter"
				if filtering {
					m.SetFilter(m.filterBeforeEdit)
				}
				m.mode = ""
				m.input.Blur()
				if filtering {
					return m, m.refreshFilter()
				}
				return m, nil
			case "enter":
				if m.mode == "filter" {
					m.SetFilter(m.input.Value())
					m.mode = ""
					m.input.Blur()
					return m, m.refreshFilter()
				}
				refs, err := core.ParseBatch(m.input.Value(), m.Config.GitHubHost)
				if err != nil {
					m.notice = err.Error()
					return m, nil
				}
				n := m.add(refs)
				m.notice = fmt.Sprintf("Added %d PRs.", n)
				m.mode = ""
				m.input.Blur()
				return m, m.poll()
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			if m.mode == "filter" {
				m.SetFilter(m.input.Value())
			}
			return m, cmd
		}
		if m.help {
			if key == "?" || key == "esc" || key == "q" {
				m.help = false
			}
			if key == "Q" {
				return m, tea.Quit
			}
			return m, nil
		}
		if i := m.selected(); i >= 0 && m.detailFocus != "" {
			limit := max(0, len(m.details(m.PRs[i], true))-m.detailHeight())
			handled := true
			switch key {
			case "left", "esc":
				m.expanded[m.detailFocus] = false
				m.detailFocus, m.detailScroll = "", 0
				return m, nil
			case "up", "k", "[":
				m.detailScroll--
			case "down", "j", "]":
				m.detailScroll++
			case "pgup":
				m.detailScroll -= m.detailHeight()
			case "pgdown":
				m.detailScroll += m.detailHeight()
			case "home", "g":
				m.detailScroll = 0
			case "end", "G":
				m.detailScroll = limit
			case "right":
				return m, nil
			default:
				handled = false
			}
			if handled {
				m.detailScroll = max(0, min(m.detailScroll, limit))
				return m, nil
			}
		}
		switch key {
		case "Q", "q":
			m.QuitReason = "quit"
			return m, tea.Quit
		case "?":
			m.help = true
		case "a":
			return m, m.startInput("add")
		case "v", "ctrl+v":
			return m, m.importClipboard()
		case "d":
			return m, m.discover()
		case "/":
			return m, m.startInput("filter")
		case "esc":
			m.SetFilter("")
			m.notice = ""
			return m, m.refreshFilter()
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
			m.jobCursor = 0
			m.detailScroll = 0
		case "down", "j":
			m.cursor++
			m.selected()
			m.jobCursor = 0
			m.detailScroll = 0
		case "pgup":
			m.cursor = max(0, m.cursor-m.visible())
			m.jobCursor = 0
			m.detailScroll = 0
		case "pgdown":
			m.cursor += m.visible()
			m.selected()
			m.jobCursor = 0
			m.detailScroll = 0
		case "home", "g":
			m.cursor = 0
			m.jobCursor = 0
			m.detailScroll = 0
		case "end", "G":
			m.cursor = len(m.PRs)
			m.selected()
			m.jobCursor = 0
			m.detailScroll = 0
		case "enter", " ", "right", "left":
			if i := m.selected(); i >= 0 {
				url := m.PRs[i].Ref.URL
				if key == "right" {
					m.expanded[url] = true
					m.detailFocus = url
				} else if key == "left" {
					m.expanded[url] = false
				} else {
					m.expanded[url] = !m.expanded[url]
					m.detailFocus = ""
				}
				m.detailScroll = 0
			}
		case "c", "C":
			return m, m.copyCommand(key == "C")
		case "y", "Y":
			return m, m.copyJSON(key == "Y")
		case "tab":
			m.jobCursor++
			m.detailScroll = 0
		case "shift+tab":
			m.jobCursor--
			m.detailScroll = 0
		case "s":
			if m.Config.Sort == "repo" {
				m.Config.Sort = "progress"
			} else {
				m.Config.Sort = "repo"
			}
		case "r":
			m.Config.Descending = !m.Config.Descending
		case "p":
			m.paused = !m.paused
			if !m.paused {
				return m, m.poll()
			}
		case "R":
			return m, m.poll()
		case "x":
			if i := m.selected(); i >= 0 {
				m.account(time.Now())
				m.detailFocus, m.detailScroll = "", 0
				m.PRs[i].Removed, m.PRs[i].Expired = true, false
				m.Dismissed[strings.ToLower(m.PRs[i].Ref.URL)] = m.PRs[i].Ref
				m.notice = "PR dismissed; discovery will skip it. Paste it again to restore it."
				m.save()
			}
		case "o", "b":
			i := m.selected()
			if i < 0 {
				return m, nil
			}
			raw := m.PRs[i].Ref.URL
			if key == "b" {
				job := m.selectedJob(m.PRs[i])
				if job == nil || job.URL == "" {
					m.notice = "No build URL for this check."
					return m, nil
				}
				raw = job.URL
			}
			if m.Actions.Open == nil {
				m.notice = "Browser opening is unavailable."
				return m, nil
			}
			ctx, open := m.Context, m.Actions.Open
			return m, func() tea.Msg { return openMsg{Err: open(ctx, raw)} }
		}
	}
	return m, nil
}
func (m *Model) detailHeight() int { return max(1, m.visible()-2) }
func (m *Model) visible() int      { return max(1, m.height-8-len(m.footer())) }
func (m *Model) selectedJob(p core.PR) *core.Job {
	if len(p.Jobs) == 0 {
		return nil
	}
	m.jobCursor = (m.jobCursor%len(p.Jobs) + len(p.Jobs)) % len(p.Jobs)
	return &p.Jobs[m.jobCursor]
}
