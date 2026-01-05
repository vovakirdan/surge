package main

import (
	"fmt"
	"os"
	"strings"
)

type uiMode string

const (
	uiModeAuto uiMode = "auto"
	uiModeOn   uiMode = "on"
	uiModeOff  uiMode = "off"
)

func readUIMode(value string) (uiMode, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "auto":
		return uiModeAuto, nil
	case "on":
		return uiModeOn, nil
	case "off":
		return uiModeOff, nil
	default:
		return "", fmt.Errorf("invalid --ui value %q (expected auto|on|off)", value)
	}
}

func shouldUseTUI(mode uiMode) bool {
	switch mode {
	case uiModeOn:
		return true
	case uiModeOff:
		return false
	default:
		return isTerminal(os.Stdout)
	}
}
