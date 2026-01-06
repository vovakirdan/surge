package observ

import (
	"fmt"
	"time"
)

// Phase records the duration and metadata of a compilation phase.
type Phase struct {
	Name  string
	Start time.Time
	Dur   time.Duration
	Note  string
}

// Timer tracks the execution time of multiple compilation phases.
type Timer struct {
	phases []Phase
}

// NewTimer creates a new empty Timer.
func NewTimer() *Timer { return &Timer{phases: make([]Phase, 0, 8)} }

// Begin starts a new phase and returns its index.
func (t *Timer) Begin(name string) int {
	t.phases = append(t.phases, Phase{Name: name, Start: time.Now()})
	return len(t.phases) - 1
}

// End finishes a phase by its index.
func (t *Timer) End(idx int, note string) {
	if idx < 0 || idx >= len(t.phases) {
		return
	}
	p := &t.phases[idx]
	p.Dur = time.Since(p.Start)
	p.Note = note
}

// Summary returns a human-readable string summarizing all tracked phases.
func (t *Timer) Summary() string {
	report := t.Report()
	out := "timings:\n"
	for _, p := range report.Phases {
		out += fmt.Sprintf("  %-20s %7.2f ms", p.Name, p.DurationMS)
		if p.Note != "" {
			out += "  // " + p.Note
		}
		out += "\n"
	}
	out += fmt.Sprintf("  %-20s %7.2f ms\n", "total", report.TotalMS)
	return out
}

// PhaseReport представляет сжатую информацию о фазе таймера для сериализации.
type PhaseReport struct {
	Name       string  `json:"name"`
	DurationMS float64 `json:"duration_ms"`
	Note       string  `json:"note,omitempty"`
}

// Report описывает агрегированные данные таймера.
type Report struct {
	TotalMS float64       `json:"total_ms"`
	Phases  []PhaseReport `json:"phases"`
}

// Report формирует срез фаз и общую длительность в миллисекундах.
func (t *Timer) Report() Report {
	if len(t.phases) == 0 {
		return Report{}
	}
	report := Report{
		Phases: make([]PhaseReport, len(t.phases)),
	}
	var total time.Duration
	for i, phase := range t.phases {
		total += phase.Dur
		report.Phases[i] = PhaseReport{
			Name:       phase.Name,
			DurationMS: durationToMillis(phase.Dur),
			Note:       phase.Note,
		}
	}
	report.TotalMS = durationToMillis(total)
	return report
}

func durationToMillis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
