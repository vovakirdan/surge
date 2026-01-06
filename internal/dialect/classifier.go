package dialect

// Classification is the result of scoring evidence for a file.
type Classification struct {
	Kind            Kind
	Score           int
	TotalScore      int
	Confidence      float64
	RunnerUp        Kind
	RunnerUpScore   int
	ObservedSignals int
}

// Classifier scores evidence and chooses a dominant dialect.
// It is intentionally simple; callers apply their own thresholds/policies.
type Classifier struct{}

// Classify scores the evidence and returns the most likely dialect.
func (Classifier) Classify(e *Evidence) Classification {
	if e == nil || len(e.hints) == 0 {
		return Classification{Kind: Unknown}
	}

	var scores [dialectKindCount]int
	total := 0
	observed := 0
	for _, h := range e.hints {
		observed++
		if h.Score <= 0 {
			continue
		}
		switch h.Dialect {
		case Rust:
			scores[Rust] += h.Score
			total += h.Score
		case Go:
			scores[Go] += h.Score
			total += h.Score
		case TypeScript:
			scores[TypeScript] += h.Score
			total += h.Score
		case Python:
			scores[Python] += h.Score
			total += h.Score
		}
	}

	bestKind := Unknown
	bestScore := 0
	runnerKind := Unknown
	runnerScore := 0
	for kind := Rust; kind < dialectKindCount; kind++ {
		score := scores[int(kind)]
		if score > bestScore {
			runnerKind, runnerScore = bestKind, bestScore
			bestKind, bestScore = kind, score
			continue
		}
		if score > runnerScore {
			runnerKind, runnerScore = kind, score
		}
	}

	conf := 0.0
	if total > 0 {
		conf = float64(bestScore) / float64(total)
	}

	return Classification{
		Kind:            bestKind,
		Score:           bestScore,
		TotalScore:      total,
		Confidence:      conf,
		RunnerUp:        runnerKind,
		RunnerUpScore:   runnerScore,
		ObservedSignals: observed,
	}
}
