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
	var printErr error
	if timings.Has(buildpipeline.StageParse) {
		_, printErr = fmt.Fprintf(out, "parsed %.1f ms\n", toMillis(timings.Duration(buildpipeline.StageParse)))
		if printErr != nil {
			panic(printErr)
		}
	}
	if timings.Has(buildpipeline.StageDiagnose) {
		_, printErr = fmt.Fprintf(out, "diagnose %.1f ms\n", toMillis(timings.Duration(buildpipeline.StageDiagnose)))
		if printErr != nil {
			panic(printErr)
		}
	}
	if includeBuilt && (timings.Has(buildpipeline.StageLower) || timings.Has(buildpipeline.StageBuild) || timings.Has(buildpipeline.StageLink)) {
		built := timings.Sum(buildpipeline.StageLower, buildpipeline.StageBuild, buildpipeline.StageLink)
		_, printErr = fmt.Fprintf(out, "built %.1f ms\n", toMillis(built))
		if printErr != nil {
			panic(printErr)
		}
	}
	if includeRun && timings.Has(buildpipeline.StageRun) {
		_, printErr = fmt.Fprintf(out, "ran %.1f ms\n", toMillis(timings.Duration(buildpipeline.StageRun)))
		if printErr != nil {
			panic(printErr)
		}
	}
}

func toMillis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
