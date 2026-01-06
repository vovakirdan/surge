package main

import "surge/internal/buildpipeline"

func toPipelineDirInfo(info *runDirInfo) *buildpipeline.DirInfo {
	if info == nil {
		return nil
	}
	return &buildpipeline.DirInfo{
		Path:      info.path,
		FileCount: info.fileCount,
	}
}
