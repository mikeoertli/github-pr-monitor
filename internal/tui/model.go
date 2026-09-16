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
}
type Model struct {
	Config                                      config.Config
	PRs                                         []core.PR
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
	help, busy, importing, paused, refreshAgain bool
	notice                                      string
	lastTick, lastPoll, lastSave                time.Time
	demoStep                                    int
}
type tickMsg time.Time
type pollMsg struct{ PRs []core.PR }
type importMsg struct {
	Refs  []core.Ref
	Err   error
	Label string
}
type openMsg struct{ Err error }

func New(ctx context.Context, c config.Config, prs []core.PR, source Source, actions Actions, state string, demo bool) *Model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 32768
	input.Width = 70
	now := time.Now()
	return &Model{Context: ctx, Config: c, PRs: prs, Source: source, Actions: actions, StatePath: state, Demo: demo, Started: now, lastTick: now, width: 120, height: 30, input: input}
}
func (m *Model) Init() tea.Cmd { return tea.Batch(tick(), m.poll()) }
func tick() tea.Cmd            { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }
func (m *Model) poll() tea.Cmd {
	if m.busy {
		m.refreshAgain = true
		return nil
	}
	refs := []core.Ref{}
	for _, p := range m.PRs {
		if !p.Removed {
			refs = append(refs, p.Ref)
		}
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
		found := false
		for i, p := range m.PRs {
			if strings.EqualFold(p.Ref.URL, ref.URL) {
				found = true
				if p.Removed {
					m.PRs[i].Removed = false
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
	m.SaveError = core.SaveSession(m.StatePath, m.PRs)
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
	for i := range m.PRs {
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
func (m *Model) Finish() { m.account(time.Now()); m.save() }
func (m *Model) selected() int {
	rows := core.Sorted(m.PRs, m.filter, m.Config.Sort, m.Config.Descending)
	if len(rows) == 0 {
		return -1
	}
	m.cursor = max(0, min(m.cursor, len(rows)-1))
	return rows[m.cursor]
}
func (m *Model) startInput(mode string) tea.Cmd {
	m.mode = mode
	m.input.SetValue("")
	m.input.Placeholder = "PR URLs or owner/repo#123 (multiple accepted)"
	if mode == "filter" {
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
		if !m.paused && now.Sub(m.lastPoll) >= m.Config.PollInterval() && !m.busy {
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
		m.save()
		if m.refreshAgain {
			m.refreshAgain = false
			return m, m.poll()
		}
		if m.mode == "" && !m.importing && !m.paused && core.ShouldQuit(m.PRs, m.Config.AutoQuit) {
			m.QuitReason = "auto-quit: " + m.Config.AutoQuit
			return m, tea.Quit
		}
	case importMsg:
		m.importing = false
		if msg.Err != nil {
			m.notice = core.Clean(msg.Err.Error())
			break
		}
		n := m.add(msg.Refs)
		m.notice = fmt.Sprintf("Added %d PRs from %s (%d already monitored).", n, msg.Label, len(msg.Refs)-n)
		if msg.Label == "discovery" && len(msg.Refs) >= m.Config.DiscoveryLimit {
			m.notice += " Discovery limit reached; increase discovery_limit for more."
		}
		return m, m.poll()
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
				m.mode = ""
				m.input.Blur()
				return m, nil
			case "enter":
				if m.mode == "filter" {
					m.filter = m.input.Value()
					m.cursor = 0
					m.mode = ""
					m.input.Blur()
					return m, nil
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
				m.filter = m.input.Value()
				m.cursor = 0
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
			m.filter = ""
			m.notice = ""
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
			m.jobCursor = 0
		case "down", "j":
			m.cursor++
			m.selected()
			m.jobCursor = 0
		case "pgup":
			m.cursor = max(0, m.cursor-m.visible())
			m.jobCursor = 0
		case "pgdown":
			m.cursor += m.visible()
			m.selected()
			m.jobCursor = 0
		case "home", "g":
			m.cursor = 0
			m.jobCursor = 0
		case "end", "G":
			m.cursor = len(m.PRs)
			m.selected()
			m.jobCursor = 0
		case "tab", "enter":
			m.jobCursor++
		case "shift+tab":
			m.jobCursor--
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
				m.PRs[i].Removed = true
				m.notice = "PR removed from monitoring; retained in this session's summary."
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
			ctx, open := m.Context, m.Actions.Open
			return m, func() tea.Msg { return openMsg{Err: open(ctx, raw)} }
		}
	}
	return m, nil
}
func (m *Model) visible() int { return max(1, m.height-12) }
func (m *Model) selectedJob(p core.PR) *core.Job {
	if len(p.Jobs) == 0 {
		return nil
	}
	m.jobCursor = (m.jobCursor%len(p.Jobs) + len(p.Jobs)) % len(p.Jobs)
	return &p.Jobs[m.jobCursor]
}
