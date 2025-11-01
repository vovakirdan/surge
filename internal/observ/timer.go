package observ

import (
	"fmt"
	"time"
)

type Phase struct {
	Name  string
	Start time.Time
	Dur   time.Duration
	Note  string
}

type Timer struct {
	phases []Phase
}

func NewTimer() *Timer { return &Timer{phases: make([]Phase, 0, 8)} }

func (t *Timer) Begin(name string) int {
	t.phases = append(t.phases, Phase{Name: name, Start: time.Now()})
	return len(t.phases) - 1
}

func (t *Timer) End(idx int, note string) {
	if idx < 0 || idx >= len(t.phases) {
		return
	}
	p := &t.phases[idx]
	p.Dur = time.Since(p.Start)
	p.Note = note
}

func (t *Timer) Summary() string {
	var total time.Duration
	for _, p := range t.phases {
		total += p.Dur
	}
	out := "timings:\n"
	for _, p := range t.phases {
		out += fmt.Sprintf("  %-20s %7.2f ms", p.Name, float64(p.Dur.Microseconds())/1000.0)
		if p.Note != "" {
			out += "  // " + p.Note
		}
		out += "\n"
	}
	out += fmt.Sprintf("  %-20s %7.2f ms\n", "total", float64(total.Microseconds())/1000.0)
	return out
}
