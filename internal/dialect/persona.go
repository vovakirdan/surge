package dialect

import (
	"fmt"
	"strings"
)

// AlienHintKind groups "alien hints" by meaning (not by diagnostic code).
// It is presentation-only: it must never affect detection logic.
type AlienHintKind uint8

const (
	// AlienHintUnknown represents an unidentified hint kind.
	AlienHintUnknown AlienHintKind = iota

	// AlienHintImplTrait represents an impl/trait hint.
	AlienHintImplTrait
	// AlienHintAttribute represents an attribute hint.
	AlienHintAttribute
	// AlienHintMacroCall represents a macro call hint.
	AlienHintMacroCall
	// AlienHintImplicitReturn represents an implicit return hint.
	AlienHintImplicitReturn
	// AlienHintGoDefer represents a Go defer hint.
	AlienHintGoDefer
	// AlienHintTSInterface represents a TypeScript interface hint.
	AlienHintTSInterface

	AlienHintPythonNoneType
	AlienHintPythonNoneAlias
)

// DialectPersona defines the personality of a dialect hint message.
type DialectPersona struct {
	Name      string
	Greetings []string
	LeadIns   []string
	CoreHints map[AlienHintKind][]string
	Closings  []string
}

// RenderInput provides data for rendering an alien hint message.
type RenderInput struct {
	Kind         AlienHintKind
	Detected     string
	SurgeExample string
}

// RenderAlienHint builds a friendly, persona-based message for an alien hint.
// It must be deterministic and must not change emission logic.
func RenderAlienHint(d Kind, in RenderInput) string {
	p := personaForDialect(d)
	return p.Render(in)
}

func personaForDialect(d Kind) DialectPersona {
	switch d {
	case Rust:
		return DialectPersona{
			Name:      "rust",
			Greetings: []string{"Ah, a fellow Rustacean!"},
			LeadIns:   []string{"That looks like Rust %s."},
			CoreHints: map[AlienHintKind][]string{
				AlienHintImplTrait:      {"Surge doesn’t use `impl` blocks; think `contract` requirements + `extern<T>` methods."},
				AlienHintAttribute:      {"Surge attributes start with `@` (e.g. `@align(8)`)."},
				AlienHintMacroCall:      {"Surge has no `!` macros—use a normal call instead."},
				AlienHintImplicitReturn: {"Surge requires an explicit `return` (with a semicolon) for function results."},
			},
			Closings: []string{"In Surge, try:"},
		}
	case Go:
		return DialectPersona{
			Name:      "go",
			Greetings: []string{"Oh hey, Gopher."},
			LeadIns:   []string{"I see %s."},
			CoreHints: map[AlienHintKind][]string{
				AlienHintGoDefer: {"Surge doesn’t have `defer`; use explicit cleanup or a `@raii` type."},
			},
			Closings: []string{"In Surge, try:"},
		}
	case TypeScript:
		return DialectPersona{
			Name:    "typescript",
			LeadIns: []string{"TypeScript %s detected."},
			CoreHints: map[AlienHintKind][]string{
				AlienHintTSInterface: {"Surge uses `contract` for interfaces and `type Foo = { ... };` for data shapes."},
			},
			Closings: []string{"In Surge, try:"},
		}
	case Python:
		return DialectPersona{
			Name:      "python",
			Greetings: []string{"None of that here."},
			LeadIns:   []string{"Python %s detected."},
			CoreHints: map[AlienHintKind][]string{
				AlienHintPythonNoneType:  {"In Surge, the absence type/value is `nothing`."},
				AlienHintPythonNoneAlias: {"`nothing` is already built in; the `None` alias is optional."},
			},
			Closings: []string{"In Surge, try:"},
		}
	default:
		return DialectPersona{
			Name:    "unknown",
			LeadIns: []string{"Foreign-language syntax detected."},
		}
	}
}

// Render produces the final hint message string.
func (p *DialectPersona) Render(in RenderInput) string {
	lines := make([]string, 0, 6)

	if greeting := pick(p.Greetings, int(in.Kind)); greeting != "" {
		lines = append(lines, greeting)
	}

	leadIn := formatTemplate(pick(p.LeadIns, int(in.Kind)), in.Detected)
	if leadIn != "" {
		lines = append(lines, leadIn)
	}

	core := pick(p.CoreHints[in.Kind], int(in.Kind))
	core = strings.TrimSpace(core)
	if core != "" {
		lines = append(lines, core)
	}

	example := strings.TrimSpace(in.SurgeExample)
	if example != "" {
		if closing := pick(p.Closings, int(in.Kind)); closing != "" && len(lines)+1+3 <= 6 {
			lines = append(lines, closing)
		}
		lines = append(lines, "```sg", example, "```")
	}

	return strings.Join(lines, "\n")
}

func pick(options []string, seed int) string {
	if len(options) == 0 {
		return ""
	}
	if seed < 0 {
		seed = -seed
	}
	return strings.TrimSpace(options[seed%len(options)])
}

func formatTemplate(tmpl, detected string) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	if strings.Contains(tmpl, "%s") {
		if detected == "" {
			detected = "this"
		}
		return fmt.Sprintf(tmpl, detected)
	}
	return tmpl
}
