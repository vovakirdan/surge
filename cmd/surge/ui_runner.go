package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"surge/internal/buildpipeline"
	"surge/internal/ui"
)

type buildOutcome struct {
	result buildpipeline.BuildResult
	err    error
}

type compileOutcome struct {
	result buildpipeline.CompileResult
	err    error
}

func runBuildWithUI(ctx context.Context, title string, files []string, req *buildpipeline.BuildRequest) (buildpipeline.BuildResult, error) {
	if req == nil {
		return buildpipeline.BuildResult{}, fmt.Errorf("missing build request")
	}
	events := make(chan buildpipeline.Event, 256)
	outcomeCh := make(chan buildOutcome, 1)

	go func() {
		reqCopy := *req
		reqCopy.Progress = buildpipeline.ChannelSink{Ch: events}
		res, err := buildpipeline.Build(ctx, &reqCopy)
		outcomeCh <- buildOutcome{result: res, err: err}
		close(events)
	}()

	model := ui.NewProgressModel(title, files, events)
	program := tea.NewProgram(model, tea.WithOutput(os.Stdout))
	_, uiErr := program.Run()
	outcome := <-outcomeCh
	if uiErr != nil {
		return outcome.result, uiErr
	}
	return outcome.result, outcome.err
}

func runCompileWithUI(ctx context.Context, title string, files []string, req *buildpipeline.CompileRequest) (buildpipeline.CompileResult, error) {
	if req == nil {
		return buildpipeline.CompileResult{}, fmt.Errorf("missing compile request")
	}
	events := make(chan buildpipeline.Event, 256)
	outcomeCh := make(chan compileOutcome, 1)

	go func() {
		reqCopy := *req
		reqCopy.Progress = buildpipeline.ChannelSink{Ch: events}
		res, err := buildpipeline.Compile(ctx, &reqCopy)
		outcomeCh <- compileOutcome{result: res, err: err}
		close(events)
	}()

	model := ui.NewProgressModel(title, files, events)
	program := tea.NewProgram(model, tea.WithOutput(os.Stdout))
	_, uiErr := program.Run()
	outcome := <-outcomeCh
	if uiErr != nil {
		return outcome.result, uiErr
	}
	return outcome.result, outcome.err
}
