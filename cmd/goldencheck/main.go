package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"surge/internal/goldencheck"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "goldencheck: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return fmt.Errorf("usage: goldencheck <check|update> [flags] [-- command ...]")
	}
	command := arguments[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	repoRoot := flags.String("repo", ".", "repository root")
	expectations := flags.String("expectations", "testdata/golden.expectations.json", "expectation manifest")
	runs := flags.Int("runs", 2, "serialized generator runs for check")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	options := goldencheck.Options{
		RepoRoot:         *repoRoot,
		ExpectationsPath: *expectations,
		Command:          flags.Args(),
		Runs:             *runs,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
	}
	switch command {
	case "check":
		return goldencheck.Check(context.Background(), &options)
	case "update":
		return goldencheck.Update(context.Background(), &options)
	default:
		return fmt.Errorf("unknown command %q; want check or update", command)
	}
}
