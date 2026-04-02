package main

import (
	"errors"
	"path/filepath"

	"surge/internal/project"
)

type commandTarget struct {
	targetPath   string
	dirInfo      *runDirInfo
	baseDir      string
	rootKind     project.ModuleKind
	outputName   string
	manifestRoot string
	usesManifest bool
}

func resolveCommandTarget(argsBeforeDash []string) (commandTarget, error) {
	if hasExplicitTargetArg(argsBeforeDash) {
		inputPath := argsBeforeDash[0]
		targetPath, dirInfo, err := resolveRunTarget(inputPath)
		if err != nil {
			return commandTarget{}, err
		}
		return commandTarget{
			targetPath: targetPath,
			dirInfo:    dirInfo,
			outputName: outputNameFromPath(inputPath, dirInfo),
		}, nil
	}

	manifest, manifestFound, err := loadProjectManifest(".")
	if err != nil {
		return commandTarget{}, err
	}
	if !manifestFound {
		return commandTarget{}, errors.New(noSurgeTomlMessage)
	}

	targetPath, dirInfo, err := resolveProjectRunTarget(manifest)
	if err != nil {
		return commandTarget{}, err
	}
	return commandTarget{
		targetPath:   targetPath,
		dirInfo:      dirInfo,
		baseDir:      manifest.Root,
		rootKind:     project.ModuleKindBinary,
		outputName:   manifest.Config.Package.Name,
		manifestRoot: manifest.Root,
		usesManifest: true,
	}, nil
}

func hasExplicitTargetArg(argsBeforeDash []string) bool {
	if len(argsBeforeDash) == 0 {
		return false
	}
	return filepath.Clean(argsBeforeDash[0]) != "."
}
