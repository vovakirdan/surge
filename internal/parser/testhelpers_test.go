package parser

import (
	"fmt"
	"strings"

	"surge/internal/diag"
)

func diagnosticsSummary(bag *diag.Bag) string {
	if bag == nil {
		return "<nil bag>"
	}
	diags := bag.Items()
	if len(diags) == 0 {
		return "<none>"
	}
	lines := make([]string, len(diags))
	for i, d := range diags {
		lines[i] = fmt.Sprintf("[%s] %s", d.Code.ID(), d.Message)
	}
	return strings.Join(lines, "; ")
}
