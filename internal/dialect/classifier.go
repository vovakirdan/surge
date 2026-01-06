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
		if h.Dialect <= Unknown || h.Dialect >= dialectKindCount {
			continue
		}
		scores[h.Dialect] += h.Score
		total += h.Score
	}

	bestKind := Unknown
	bestScore := 0
	runnerKind := Unknown
	runnerScore := 0
	for k := Rust; k < dialectKindCount; k++ {
		score := scores[k]
		if score > bestScore {
			runnerKind, runnerScore = bestKind, bestScore
			bestKind, bestScore = k, score
			continue
		}
		if score > runnerScore {
			runnerKind, runnerScore = k, score
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
