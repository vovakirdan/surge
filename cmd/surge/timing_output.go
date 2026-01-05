package main

import (
	"fmt"
	"io"
	"time"

	"surge/internal/buildpipeline"
)

func printStageTimings(out io.Writer, timings buildpipeline.Timings, includeBuilt, includeRun bool) {
	if out == nil {
		return
	}
	if timings.Has(buildpipeline.StageParse) {
		fmt.Fprintf(out, "parsed %.1f ms\n", toMillis(timings.Duration(buildpipeline.StageParse)))
	}
	if timings.Has(buildpipeline.StageDiagnose) {
		fmt.Fprintf(out, "diagnose %.1f ms\n", toMillis(timings.Duration(buildpipeline.StageDiagnose)))
	}
	if includeBuilt && (timings.Has(buildpipeline.StageLower) || timings.Has(buildpipeline.StageBuild) || timings.Has(buildpipeline.StageLink)) {
		built := timings.Sum(buildpipeline.StageLower, buildpipeline.StageBuild, buildpipeline.StageLink)
		fmt.Fprintf(out, "built %.1f ms\n", toMillis(built))
	}
	if includeRun && timings.Has(buildpipeline.StageRun) {
		fmt.Fprintf(out, "ran %.1f ms\n", toMillis(timings.Duration(buildpipeline.StageRun)))
	}
}

func toMillis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
