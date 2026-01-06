package buildpipeline

import (
	"context"
	"fmt"
	"os"
	"time"

	"surge/internal/diag"
	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/observ"
	"surge/internal/project"
)

// DirInfo describes a directory run target.
type DirInfo struct {
	Path      string
	FileCount int
}

// CompileRequest configures the shared compilation pipeline.
type CompileRequest struct {
	TargetPath            string
	BaseDir               string
	RootKind              project.ModuleKind
	MaxDiagnostics        int
	DirInfo               *DirInfo
	AllowDiagnosticsError bool
	Progress              ProgressSink
	Files                 []string
}

// CompileResult captures compilation artefacts and stage timings.
type CompileResult struct {
	Diagnose *driver.DiagnoseResult
	MIR      *mir.Module
	Timings  Timings
}

// Compile runs parsing, diagnostics, and lowering into MIR.
func Compile(ctx context.Context, req *CompileRequest) (CompileResult, error) {
	var result CompileResult
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil {
		return result, fmt.Errorf("missing compile request")
	}
	if req.TargetPath == "" {
		return result, fmt.Errorf("missing target path")
	}

	if req.Progress != nil && len(req.Files) > 0 {
		emitQueued(req.Progress, req.Files)
	}
	phaseProgress := &phaseObserver{
		sink:  req.Progress,
		files: req.Files,
	}

	opts := driver.DiagnoseOptions{
		Stage:              driver.DiagnoseStageSema,
		MaxDiagnostics:     req.MaxDiagnostics,
		EmitHIR:            true,
		EmitInstantiations: true,
		BaseDir:            req.BaseDir,
		RootKind:           req.RootKind,
		EnableTimings:      true,
		PhaseObserver:      phaseProgress.OnPhase,
	}

	diagRes, err := driver.DiagnoseWithOptions(ctx, req.TargetPath, &opts)
	if err != nil {
		result.Diagnose = diagRes
		emitStage(req.Progress, req.Files, StageDiagnose, StatusError, err, 0)
		return result, err
	}
	result.Diagnose = diagRes
	recordDiagnoseTimings(&result, diagRes.TimingReport)
	expandProgressFiles(req, phaseProgress, diagRes)

	if diagRes.Bag != nil && diagRes.Bag.HasErrors() {
		for _, d := range diagRes.Bag.Items() {
			if d.Severity != diag.SevError {
				continue
			}
			fmt.Fprintln(os.Stderr, d.Message)
		}
		if !req.AllowDiagnosticsError {
			err = fmt.Errorf("diagnostics reported errors")
		}
	}
	if err != nil {
		emitStage(req.Progress, req.Files, StageDiagnose, StatusError, err, 0)
		return result, err
	}

	if validateErr := ValidateEntrypoints(diagRes); validateErr != nil {
		emitStage(req.Progress, req.Files, StageDiagnose, StatusError, validateErr, 0)
		return result, validateErr
	}
	if req.DirInfo != nil && req.DirInfo.FileCount > 1 {
		meta := diagRes.RootModuleMeta()
		if meta == nil {
			err = fmt.Errorf("failed to resolve module metadata for %q", req.DirInfo.Path)
			emitStage(req.Progress, req.Files, StageDiagnose, StatusError, err, 0)
			return result, err
		}
		if !meta.HasModulePragma {
			err = fmt.Errorf("directory %q is not a module; add pragma module/binary to all .sg files or run a file", req.DirInfo.Path)
			emitStage(req.Progress, req.Files, StageDiagnose, StatusError, err, 0)
			return result, err
		}
	}

	if diagRes.HIR == nil {
		err = fmt.Errorf("HIR not available")
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}
	if diagRes.Instantiations == nil {
		err = fmt.Errorf("instantiation map not available")
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}
	if diagRes.Sema == nil {
		err = fmt.Errorf("semantic analysis result not available")
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}

	if req.Progress != nil && !phaseProgress.lowerStarted {
		emitStage(req.Progress, req.Files, StageLower, StatusWorking, nil, 0)
	}
	lowerStart := time.Now()

	hirModule, err := driver.CombineHIRWithModules(ctx, diagRes)
	if err != nil {
		err = fmt.Errorf("HIR merge failed: %w", err)
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}
	if hirModule == nil {
		hirModule = diagRes.HIR
	}

	mm, err := mono.MonomorphizeModule(hirModule, diagRes.Instantiations, diagRes.Sema, mono.Options{
		MaxDepth: 64,
	})
	if err != nil {
		err = fmt.Errorf("monomorphization failed: %w", err)
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}

	mirMod, err := mir.LowerModule(mm, diagRes.Sema)
	if err != nil {
		err = fmt.Errorf("MIR lowering failed: %w", err)
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}

	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}

	if err := mir.LowerAsyncStateMachine(mirMod, diagRes.Sema, diagRes.Symbols.Table); err != nil {
		err = fmt.Errorf("async lowering failed: %w", err)
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
	}

	if err := mir.Validate(mirMod, diagRes.Sema.TypeInterner); err != nil {
		err = fmt.Errorf("MIR validation failed: %w", err)
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}

	result.MIR = mirMod
	result.Timings.Set(StageLower, time.Since(lowerStart))
	return result, nil
}

type phaseObserver struct {
	sink            ProgressSink
	files           []string
	parseStarted    bool
	diagnoseStarted bool
	lowerStarted    bool
}

// OnPhase updates the progress UI based on compiler phase events.
func (p *phaseObserver) OnPhase(ev driver.PhaseEvent) {
	if p == nil || p.sink == nil {
		return
	}
	if ev.Status != driver.PhaseStart {
		return
	}
	switch ev.Name {
	case "load_file", "tokenize", "parse":
		if p.parseStarted {
			return
		}
		p.parseStarted = true
		emitStage(p.sink, p.files, StageParse, StatusWorking, nil, 0)
	case "imports_graph", "symbols", "sema":
		if p.diagnoseStarted {
			return
		}
		p.diagnoseStarted = true
		emitStage(p.sink, p.files, StageDiagnose, StatusWorking, nil, 0)
	case "hir":
		if p.lowerStarted {
			return
		}
		p.lowerStarted = true
		emitStage(p.sink, p.files, StageLower, StatusWorking, nil, 0)
	}
}

func recordDiagnoseTimings(result *CompileResult, report observ.Report) {
	if result == nil {
		return
	}
	if len(report.Phases) == 0 {
		return
	}
	parse := sumDiagnosePhase(report, "load_file", "tokenize", "parse")
	total := durationFromMillis(report.TotalMS)
	diagnose := total - parse
	if diagnose < 0 {
		diagnose = 0
	}
	result.Timings.Set(StageParse, parse)
	result.Timings.Set(StageDiagnose, diagnose)
}

func sumDiagnosePhase(report observ.Report, names ...string) time.Duration {
	if len(report.Phases) == 0 || len(names) == 0 {
		return 0
	}
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[name] = struct{}{}
	}
	var total time.Duration
	for _, phase := range report.Phases {
		if _, ok := nameSet[phase.Name]; !ok {
			continue
		}
		total += durationFromMillis(phase.DurationMS)
	}
	return total
}

func durationFromMillis(ms float64) time.Duration {
	return time.Duration(ms * float64(time.Millisecond))
}

func emitQueued(sink ProgressSink, files []string) {
	if sink == nil {
		return
	}
	for _, file := range files {
		sink.OnEvent(Event{File: file, Stage: StageParse, Status: StatusQueued})
	}
}

func emitStage(sink ProgressSink, files []string, stage Stage, status Status, err error, elapsed time.Duration) {
	if sink == nil {
		return
	}
	sink.OnEvent(Event{Stage: stage, Status: status, Err: err, Elapsed: elapsed})
	for _, file := range files {
		sink.OnEvent(Event{File: file, Stage: stage, Status: status, Err: err, Elapsed: elapsed})
	}
}
