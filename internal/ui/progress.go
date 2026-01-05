package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
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
	prog       progress.Model
	items      []fileItem
	index      map[string]int
	stageLabel string
	width      int
	done       bool
}

type fileItem struct {
	path   string
	status string
	stage  buildpipeline.Stage
}

type eventMsg buildpipeline.Event
type doneMsg struct{}

// NewProgressModel returns a Bubble Tea model that renders pipeline progress.
func NewProgressModel(title string, files []string, events <-chan buildpipeline.Event) tea.Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	prog := progress.New(progress.WithDefaultGradient())
	prog.Width = 76 // Default width

	items := make([]fileItem, 0, len(files))
	index := make(map[string]int, len(files))
	for i, file := range files {
		items = append(items, fileItem{path: file, status: "queued", stage: buildpipeline.Stage(0)})
		index[file] = i
	}
	return &progressModel{
		title:   title,
		events:  events,
		spinner: sp,
		prog:    prog,
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
		cmd := m.applyEvent(ev)
		return m, tea.Batch(cmd, m.listenForEvent())
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
			m.prog.Width = msg.Width - 4
		}
		return m, nil
	case progress.FrameMsg:
		progressModel, cmd := m.prog.Update(msg)
		m.prog = progressModel.(progress.Model)
		return m, cmd
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
		statusStyled := styleStatus(status).Render(fmt.Sprintf("%12s", status))
		line := fmt.Sprintf("  %s %s", statusStyled, name)
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.done {
		b.WriteString(m.prog.ViewAs(1.0))
	} else {
		b.WriteString(m.prog.View())
	}
	b.WriteString("\n")

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

func (m *progressModel) applyEvent(ev buildpipeline.Event) tea.Cmd {
	label := statusLabel(ev.Stage, ev.Status)
	if ev.File == "" {
		if label != "" {
			m.stageLabel = label
		}
		return nil
	}
	idx, ok := m.index[ev.File]
	if !ok {
		return nil
	}
	if label != "" {
		m.items[idx].status = label
		m.items[idx].stage = ev.Stage
	}
	// If the status is explicit success/error, mark it as fully done
	if ev.Status == buildpipeline.StatusDone || ev.Status == buildpipeline.StatusError {
		// We can use a sentinel stage or just logic in calculation
	}

	// Calculate progress
	if len(m.items) > 0 {
		totalProgress := 0.0
		for _, item := range m.items {
			if item.status == "done" || item.status == "error" {
				totalProgress += 1.0
			} else {
				totalProgress += progressFromStage(item.stage)
			}
		}
		pct := totalProgress / float64(len(m.items))
		return m.prog.SetPercent(pct)
	}
	return nil
}

func progressFromStage(stage buildpipeline.Stage) float64 {
	switch stage {
	case buildpipeline.StageParse:
		return 0.1
	case buildpipeline.StageDiagnose:
		return 0.3
	case buildpipeline.StageLower:
		return 0.5
	case buildpipeline.StageBuild:
		return 0.8
	case buildpipeline.StageLink:
		return 0.9
	case buildpipeline.StageRun:
		return 0.95
	default:
		return 0.0
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
