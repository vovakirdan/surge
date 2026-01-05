package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"surge/internal/buildpipeline"
)

type progressModel struct {
	title      string
	events     <-chan buildpipeline.Event
	spinner    spinner.Model
	items      []fileItem
	index      map[string]int
	stageLabel string
	width      int
	done       bool
}

type fileItem struct {
	path   string
	status string
}

type eventMsg buildpipeline.Event
type doneMsg struct{}

// NewProgressModel returns a Bubble Tea model that renders pipeline progress.
func NewProgressModel(title string, files []string, events <-chan buildpipeline.Event) tea.Model {
	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	items := make([]fileItem, 0, len(files))
	index := make(map[string]int, len(files))
	for i, file := range files {
		items = append(items, fileItem{path: file, status: "queued"})
		index[file] = i
	}
	return &progressModel{
		title:   title,
		events:  events,
		spinner: sp,
		items:   items,
		index:   index,
		width:   80,
	}
}

func (m *progressModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.listenForEvent())
}

func (m *progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case eventMsg:
		ev := buildpipeline.Event(msg)
		m.applyEvent(ev)
		cmd := m.listenForEvent()
		return m, cmd
	case doneMsg:
		m.done = true
		return m, tea.Quit
	case spinner.TickMsg:
		if m.done {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		return m, nil
	}
	return m, nil
}

func (m *progressModel) View() string {
	if len(m.items) == 0 {
		return ""
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))
	header := m.title
	if m.stageLabel != "" {
		header = fmt.Sprintf("%s (%s)", header, m.stageLabel)
	}
	if m.done {
		header = fmt.Sprintf("done: %s", header)
	} else {
		header = fmt.Sprintf("%s %s", m.spinner.View(), header)
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(header))
	b.WriteString("\n\n")

	statusWidth := 12
	nameWidth := m.width - statusWidth - 4
	if nameWidth < 20 {
		nameWidth = 20
	}

	for _, item := range m.items {
		name := truncate(item.path, nameWidth)
		status := item.status
		line := fmt.Sprintf("  %-*s %s", nameWidth, name, styleStatus(status).Render(status))
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m *progressModel) listenForEvent() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.events
		if !ok {
			return doneMsg{}
		}
		return eventMsg(ev)
	}
}

func (m *progressModel) applyEvent(ev buildpipeline.Event) {
	label := statusLabel(ev.Stage, ev.Status)
	if ev.File == "" {
		if label != "" {
			m.stageLabel = label
		}
		return
	}
	idx, ok := m.index[ev.File]
	if !ok {
		return
	}
	if label != "" {
		m.items[idx].status = label
	}
}

func statusLabel(stage buildpipeline.Stage, status buildpipeline.Status) string {
	switch status {
	case buildpipeline.StatusQueued:
		return "queued"
	case buildpipeline.StatusDone:
		return "done"
	case buildpipeline.StatusError:
		return "error"
	case buildpipeline.StatusWorking:
		return stageLabel(stage)
	default:
		return ""
	}
}

func stageLabel(stage buildpipeline.Stage) string {
	switch stage {
	case buildpipeline.StageParse:
		return "parsing"
	case buildpipeline.StageDiagnose:
		return "diagnosing"
	case buildpipeline.StageLower:
		return "lowering"
	case buildpipeline.StageBuild, buildpipeline.StageLink:
		return "building"
	case buildpipeline.StageRun:
		return "running"
	default:
		return ""
	}
}

func styleStatus(status string) lipgloss.Style {
	switch status {
	case "done":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	case "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	case "building", "parsing", "diagnosing", "lowering", "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	}
}

func truncate(value string, width int) string {
	if width <= 0 {
		return value
	}
	if runewidth.StringWidth(value) <= width {
		return value
	}
	if width <= 3 {
		return runewidth.Truncate(value, width, "")
	}
	return runewidth.Truncate(value, width-3, "...")
}
