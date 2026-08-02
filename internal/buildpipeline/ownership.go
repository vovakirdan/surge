package buildpipeline

import (
	"fmt"
	"strings"

	"surge/internal/mir"
)

// OwnershipVerificationError reports MIR ownership obligations that the
// development-only verifier could not prove. Findings retain their stable MIR
// identity so callers and tests can inspect the failure without parsing text.
type OwnershipVerificationError struct {
	Findings []mir.OwnershipFinding
}

func newOwnershipVerificationError(findings []mir.OwnershipFinding) *OwnershipVerificationError {
	return &OwnershipVerificationError{
		Findings: append([]mir.OwnershipFinding(nil), findings...),
	}
}

func (e *OwnershipVerificationError) Error() string {
	if e == nil || len(e.Findings) == 0 {
		return "MIR ownership verification failed"
	}

	var out strings.Builder
	fmt.Fprintf(&out, "MIR ownership verification failed with %d finding(s)", len(e.Findings))
	for i := range e.Findings {
		finding := &e.Findings[i]
		fmt.Fprintf(&out, "\n- %s", finding.String())
		if finding.ConsumingPosition != "" {
			fmt.Fprintf(&out, " [%s]", finding.ConsumingPosition)
		}
	}
	return out.String()
}
