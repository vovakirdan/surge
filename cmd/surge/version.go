package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"surge/internal/version"
)

type versionInfo struct {
	Version   string
	GitCommit string
	GitMessage string
	BuildDate string
}

type versionOptions struct {
	format   string
	showHash bool
	showMessage bool
	showDate bool
}

type versionPayload struct {
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	Tagline   string `json:"tagline"`
	GitCommit string `json:"git_commit,omitempty"`
	GitMessage string `json:"git_message,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
}

const versionTagline = "forge storms before they land"

var (
	versionFormat      string
	versionShowHash    bool
	versionShowMessage bool
	versionShowDate    bool
	versionShowFull    bool
)

func init() {
	versionCmd.Flags().BoolVar(&versionShowHash, "hash", false, "include git commit hash")
	versionCmd.Flags().BoolVar(&versionShowMessage, "message", false, "include git commit message")
	versionCmd.Flags().BoolVar(&versionShowDate, "date", false, "include build timestamp")
	versionCmd.Flags().BoolVar(&versionShowFull, "full", false, "show every recorded bit of build metadata")
	versionCmd.Flags().StringVar(&versionFormat, "format", "pretty", "output format (pretty|json)")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show surge build fingerprints",
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := versionOptions{
			format:   strings.ToLower(versionFormat),
			showHash: versionShowHash || versionShowFull,
			showMessage: versionShowMessage || versionShowFull,
			showDate: versionShowDate || versionShowFull,
		}

		switch opts.format {
		case "pretty", "json":
			// supported
		default:
			return fmt.Errorf("unsupported format %q (must be pretty or json)", versionFormat)
		}

		info := collectVersionInfo()
		if opts.format == "json" {
			return renderVersionJSON(cmd.OutOrStdout(), info, opts)
		}

		renderVersionPretty(cmd.OutOrStdout(), info, opts)
		return nil
	},
}

func collectVersionInfo() versionInfo {
	v := strings.TrimSpace(version.Version)
	if v == "" {
		v = "dev"
	}
	return versionInfo{
		Version:   v,
		GitCommit: strings.TrimSpace(version.GitCommit),
		GitMessage: strings.TrimSpace(version.GitMessage),
		BuildDate: strings.TrimSpace(version.BuildDate),
	}
}

func renderVersionPretty(out io.Writer, info versionInfo, opts versionOptions) {
	fmt.Fprintf(out, "surge %s — %s\n", info.Version, versionTagline)
	if opts.showHash {
		fmt.Fprintf(out, "commit: %s\n", valueOrUnknown(info.GitCommit))
	}
	if opts.showMessage {
		fmt.Fprintf(out, "message: %s\n", valueOrUnknown(info.GitMessage))
	}
	if opts.showDate {
		fmt.Fprintf(out, "built:  %s\n", valueOrUnknown(info.BuildDate))
	}
	if !opts.showHash && !opts.showMessage && !opts.showDate {
		fmt.Fprintln(out, "set --hash, --message, --date, or --full for more build trivia")
	}
}

func renderVersionJSON(out io.Writer, info versionInfo, opts versionOptions) error {
	payload := versionPayload{
		Tool:    "surge",
		Version: info.Version,
		Tagline: versionTagline,
	}
	if opts.showHash {
		payload.GitCommit = valueOrUnknown(info.GitCommit)
	}
	if opts.showMessage {
		payload.GitMessage = valueOrUnknown(info.GitMessage)
	}
	if opts.showDate {
		payload.BuildDate = valueOrUnknown(info.BuildDate)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func valueOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
