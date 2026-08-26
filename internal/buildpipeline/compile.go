package buildpipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"surge/internal/driver"
	"surge/internal/hir"
	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/observ"
	"surge/internal/project"
	"surge/internal/sema"
)

// monomorphizeModule is the pipeline's one entry into monomorphization.
//
// Everything a program must emit has to be reachable before this call: nothing
// behind it can go back and instantiate an implementation a later pass would
// ask for. It is a variable so a test can count entries and hold that to one.
var monomorphizeModule = mono.MonomorphizeModule

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
	// Analysis lowers every otherwise-valid source through MIR without
	// requiring an executable entrypoint. Diagnostics remain fatal and are
	// returned in Diagnose, but their messages are not echoed to stderr.
	Analysis bool
	// Dev enables development-only compiler checks. Its zero value preserves
	// the release/default pipeline and adds no ownership-verifier work.
	Dev      bool
	Progress ProgressSink
	Files    []string
	Backend  Backend
	// CrossingFormsForTest is an internal executable-crossing override used by
	// Runtime V2 proof tests before a backend capability is publicly flipped.
	// CLI/env paths must leave it nil.
	CrossingFormsForTest map[sema.CrossingLoweringKind]bool
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
	if req.Analysis && req.AllowDiagnosticsError {
		return result, fmt.Errorf("analysis compilation requires diagnostics to remain fatal")
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
		EmitHIR:            false,
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
	if diagRes == nil {
		err = fmt.Errorf("diagnostics result missing")
		emitStage(req.Progress, req.Files, StageDiagnose, StatusError, err, 0)
		return result, err
	}
	result.Diagnose = diagRes
	recordDiagnoseTimings(&result, diagRes.TimingReport)
	expandProgressFiles(req, phaseProgress, diagRes)

	addBlockingVMErrors(req, diagRes)
	if diagRes.Bag == nil || !diagRes.Bag.HasErrors() {
		addOnCrossingBackendErrors(req, diagRes)
		addSpawnOnBackendErrors(req, diagRes)
	}

	diagRes.MergeModuleDiagnostics()

	if diagRes.Bag != nil && diagRes.Bag.HasErrors() {
		if !req.Analysis {
			printBuildDiagnostics(os.Stderr, diagRes)
		}
		if !req.AllowDiagnosticsError {
			err = fmt.Errorf("diagnostics reported errors")
		}
	}
	if err != nil {
		emitStage(req.Progress, req.Files, StageDiagnose, StatusError, err, 0)
		return result, err
	}

	if !req.Analysis {
		if validateErr := ValidateEntrypoints(diagRes); validateErr != nil {
			emitStage(req.Progress, req.Files, StageDiagnose, StatusError, validateErr, 0)
			return result, validateErr
		}
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

	crossingForms := crossingFormsForRequest(req)
	if diagRes.HIR == nil {
		hirModule, lowerErr := hir.LowerWithOptions(ctx, diagRes.Builder, diagRes.FileID, diagRes.Sema, diagRes.Symbols, hir.LowerOptions{
			CrossingForms: crossingForms,
		})
		if lowerErr != nil {
			err = fmt.Errorf("HIR lowering failed: %w", lowerErr)
			emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
			return result, err
		}
		diagRes.HIR = hirModule
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

	hirModule, err := driver.CombineHIRWithModulesWithOptions(ctx, diagRes, driver.HIRCombineOptions{
		CrossingForms: crossingForms,
	})
	if err != nil {
		if errors.Is(err, driver.ErrDiagnosticsReported) {
			if !req.Analysis {
				printBuildDiagnostics(os.Stderr, diagRes)
			}
			emitStage(req.Progress, req.Files, StageDiagnose, StatusError, err, 0)
			return result, err
		}
		err = fmt.Errorf("HIR merge failed: %w", err)
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}
	if hirModule == nil {
		hirModule = diagRes.HIR
	}

	mm, err := monomorphizeModule(hirModule, diagRes.Instantiations, diagRes.Sema, mono.Options{
		MaxDepth: 64,
	})
	if err != nil {
		err = fmt.Errorf("monomorphization failed: %w", err)
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}

	mirMod, err := mir.LowerModuleWithOptions(mm, diagRes.Sema, mir.LowerOptions{
		CrossingForms: crossingForms,
	})
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
	if err := mir.FinalizeModuleMeta(mirMod, diagRes.Sema.TypeInterner, layout.X86_64LinuxGNU(),
		mir.NewOperationPlanInput(diagRes.Sema, mm)); err != nil {
		err = fmt.Errorf("MIR layout finalization failed: %w", err)
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}

	if err := mir.ValidateWithOptions(mirMod, diagRes.Sema.TypeInterner, mir.ValidateOptions{
		CrossingForms: crossingForms,
	}); err != nil {
		err = fmt.Errorf("MIR validation failed: %w", err)
		emitStage(req.Progress, req.Files, StageLower, StatusError, err, 0)
		return result, err
	}
	if req.Dev {
		if findings := mir.VerifyOwnership(mirMod, diagRes.Sema.TypeInterner, diagRes.Sema); len(findings) != 0 {
			ownershipErr := newOwnershipVerificationError(findings)
			emitStage(req.Progress, req.Files, StageLower, StatusError, ownershipErr, 0)
			return result, ownershipErr
		}
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
