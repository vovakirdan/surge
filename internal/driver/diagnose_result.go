package driver

import "surge/internal/project"

// RootModuleMeta returns metadata for the root module when available.
func (r *DiagnoseResult) RootModuleMeta() *project.ModuleMeta {
	if r == nil || r.rootRecord == nil {
		return nil
	}
	return r.rootRecord.Meta
}
