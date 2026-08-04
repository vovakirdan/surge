package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"surge/internal/abimanifest"
)

func main() {
	check := flag.Bool("check", false, "verify canonical manifest and checked-in generated views")
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if flag.NArg() != 0 {
		fail(fmt.Errorf("unexpected positional arguments"))
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fail(fmt.Errorf("resolve repository root: %w", err))
	}
	if *check {
		err = abimanifest.CheckRepository(absRoot)
	} else {
		err = abimanifest.WriteRepository(absRoot)
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "abi-manifest-gen: %v\n", err)
	os.Exit(1)
}
