package ast

import (
	"slices"
	"strings"

	"surge/internal/source"
)

// AttrTargetMask describes a set of item kinds an attribute may be applied to.
type AttrTargetMask uint16

const (
	AttrTargetNone  AttrTargetMask = 0
	AttrTargetFn    AttrTargetMask = 1 << iota // top-level or extern functions (including async)
	AttrTargetBlock                            // statement blocks (e.g. @backend on block)
	AttrTargetType                             // type declarations (struct/union/alias/newtype)
	AttrTargetField                            // struct fields
	AttrTargetParam                            // parameters (function/formal parameters)
	AttrTargetStmt                             // statement-level attributes (e.g. expression statements)
	AttrTargetLet                              // let and const declarations
)

// AttrFlag captures special handling rules beyond the basic applicability matrix.
type AttrFlag uint8

const (
	AttrFlagNone AttrFlag = 0

	// AttrFlagExternOnly marks attributes that are only valid within extern blocks (e.g. @override).
	AttrFlagExternOnly AttrFlag = 1 << iota

	// AttrFlagFnDeclOnly marks attributes that are only valid on function declarations without body (e.g. @intrinsic).
	AttrFlagFnDeclOnly
)

// AttrSpec describes a language attribute, its supported targets and special rules.
type AttrSpec struct {
	Name    string
	Targets AttrTargetMask
	Flags   AttrFlag
}

// Allows reports whether the attribute can be applied to the provided target bit.
func (spec AttrSpec) Allows(target AttrTargetMask) bool {
	return spec.Targets&target != 0
}

// HasFlag reports whether the spec contains the given flag.
func (spec AttrSpec) HasFlag(flag AttrFlag) bool {
	return spec.Flags&flag != 0
}

var attrRegistry = map[string]AttrSpec{
	"pure":          {Name: "pure", Targets: AttrTargetFn},
	"overload":      {Name: "overload", Targets: AttrTargetFn},
	"override":      {Name: "override", Targets: AttrTargetFn, Flags: AttrFlagExternOnly},
	"intrinsic":     {Name: "intrinsic", Targets: AttrTargetFn | AttrTargetType, Flags: AttrFlagFnDeclOnly},
	"entrypoint":    {Name: "entrypoint", Targets: AttrTargetFn},
	"allow_to":      {Name: "allow_to", Targets: AttrTargetFn},
	"backend":       {Name: "backend", Targets: AttrTargetFn | AttrTargetBlock},
	"deprecated":    {Name: "deprecated", Targets: AttrTargetFn | AttrTargetType | AttrTargetField | AttrTargetLet},
	"packed":        {Name: "packed", Targets: AttrTargetType | AttrTargetField},
	"align":         {Name: "align", Targets: AttrTargetType | AttrTargetField},
	"raii":          {Name: "raii", Targets: AttrTargetType},
	"arena":         {Name: "arena", Targets: AttrTargetType | AttrTargetField | AttrTargetParam},
	"weak":          {Name: "weak", Targets: AttrTargetField},
	"shared":        {Name: "shared", Targets: AttrTargetType | AttrTargetField},
	"atomic":        {Name: "atomic", Targets: AttrTargetField},
	"readonly":      {Name: "readonly", Targets: AttrTargetField},
	"hidden":        {Name: "hidden", Targets: AttrTargetFn | AttrTargetType | AttrTargetField | AttrTargetLet},
	"noinherit":     {Name: "noinherit", Targets: AttrTargetField | AttrTargetType},
	"sealed":        {Name: "sealed", Targets: AttrTargetType},
	"guarded_by":    {Name: "guarded_by", Targets: AttrTargetField},
	"requires_lock": {Name: "requires_lock", Targets: AttrTargetFn},
	"acquires_lock": {Name: "acquires_lock", Targets: AttrTargetFn},
	"releases_lock": {Name: "releases_lock", Targets: AttrTargetFn},
	"waits_on":      {Name: "waits_on", Targets: AttrTargetFn},
	"send":          {Name: "send", Targets: AttrTargetType},
	"nosend":        {Name: "nosend", Targets: AttrTargetType},
	"nonblocking":   {Name: "nonblocking", Targets: AttrTargetFn},
	"drop":          {Name: "drop", Targets: AttrTargetStmt},
	"failfast":      {Name: "failfast", Targets: AttrTargetBlock},
	"copy":          {Name: "copy", Targets: AttrTargetType},
}

// LookupAttr returns metadata for the given attribute name (case-insensitive).
func LookupAttr(name string) (AttrSpec, bool) {
	if name == "" {
		return AttrSpec{}, false
	}
	spec, ok := attrRegistry[strings.ToLower(name)]
	return spec, ok
}

// LookupAttrID resolves attribute metadata by string ID using the provided interner.
func LookupAttrID(interner *source.Interner, id source.StringID) (AttrSpec, bool) {
	if interner == nil || id == source.NoStringID {
		return AttrSpec{}, false
	}
	name, ok := interner.Lookup(id)
	if !ok {
		return AttrSpec{}, false
	}
	return LookupAttr(name)
}

// AttrSpecs returns a stable slice of all registered attribute specifications sorted by name.
func AttrSpecs() []AttrSpec {
	names := make([]string, 0, len(attrRegistry))
	for name := range attrRegistry {
		names = append(names, name)
	}
	slices.Sort(names)
	result := make([]AttrSpec, 0, len(names))
	for _, name := range names {
		result = append(result, attrRegistry[name])
	}
	return result
}
