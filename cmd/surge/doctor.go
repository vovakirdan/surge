package main

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"surge/internal/driver"
)

type doctorCheck struct {
	Name     string
	Detail   string
	Required bool
	OK       bool
}

type doctorEnv struct {
	LookupPath func(string) (string, error)
	Version    func() versionInfo
	Stdlib     func(string) string
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the local Surge installation",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		failed, err := runDoctor(cmd.OutOrStdout(), defaultDoctorEnv())
		if err != nil {
			return err
		}
		if failed > 0 {
			return fmt.Errorf("doctor found %d required problem(s)", failed)
		}
		return nil
	},
}

func defaultDoctorEnv() doctorEnv {
	return doctorEnv{
		LookupPath: exec.LookPath,
		Version:    collectVersionInfo,
		Stdlib:     driver.DetectStdlibRootFrom,
	}
}

func runDoctor(out io.Writer, env doctorEnv) (int, error) {
	if env.LookupPath == nil {
		env.LookupPath = exec.LookPath
	}
	if env.Version == nil {
		env.Version = collectVersionInfo
	}
	if env.Stdlib == nil {
		env.Stdlib = driver.DetectStdlibRootFrom
	}

	info := env.Version()
	checks := make([]doctorCheck, 0, 6)
	checks = append(checks,
		doctorCheck{Name: "surge", Detail: info.Version, Required: true, OK: strings.TrimSpace(info.Version) != ""},
		stdlibDoctorCheck(env.Stdlib(".")),
	)
	checks = append(checks, toolDoctorChecks(env.LookupPath)...)

	if _, err := fmt.Fprintln(out, "Surge doctor"); err != nil {
		return 0, err
	}
	failed := 0
	for _, check := range checks {
		status := "ok"
		if !check.OK {
			if check.Required {
				status = "error"
				failed++
			} else {
				status = "warn"
			}
		}
		if _, err := fmt.Fprintf(out, "[%s] %s: %s\n", status, check.Name, check.Detail); err != nil {
			return failed, err
		}
	}
	if failed == 0 {
		if _, err := fmt.Fprintln(out, "ready"); err != nil {
			return 0, err
		}
	}
	return failed, nil
}

func stdlibDoctorCheck(root string) doctorCheck {
	root = strings.TrimSpace(root)
	if root == "" {
		return doctorCheck{
			Name:     "stdlib",
			Detail:   "not found (set SURGE_STDLIB or reinstall Surge)",
			Required: true,
		}
	}
	return doctorCheck{
		Name:     "stdlib",
		Detail:   filepath.Clean(root),
		Required: true,
		OK:       true,
	}
}

func toolDoctorChecks(lookup func(string) (string, error)) []doctorCheck {
	return []doctorCheck{
		toolDoctorCheck(lookup, "clang", true, "required for native/LLVM backend"),
		toolDoctorCheck(lookup, "ar", true, "required for native runtime archive"),
		toolDoctorCheck(lookup, "llc", false, "optional fallback for LLVM IR compilation"),
		toolDoctorCheck(lookup, "ld.lld", false, "recommended linker from LLVM toolchain"),
	}
}

func toolDoctorCheck(lookup func(string) (string, error), name string, required bool, missingDetail string) doctorCheck {
	path, err := lookup(name)
	if err != nil || strings.TrimSpace(path) == "" {
		return doctorCheck{
			Name:     name,
			Detail:   "not found (" + missingDetail + ")",
			Required: required,
		}
	}
	return doctorCheck{
		Name:     name,
		Detail:   path,
		Required: required,
		OK:       true,
	}
}
