package mir

import (
	"fmt"
	"sync"

	"surge/internal/layout"
	"surge/internal/types"
)

// aliasOwnResolveLimit bounds alias/own unwrapping so a malformed type graph
// cannot spin here.
const aliasOwnResolveLimit = 32

// ABIDomain names which authority governs a call site's lowered signature.
//
// Three different authorities exist and they do not agree: the generated
// contract described below, the hand-written native runtime signature table,
// and dedicated foreign C lowering. Classifying a call under the wrong one is a
// silent miscompile — handing a runtime helper a hidden result destination, or
// promising a foreign callee an aggregate its target ABI passes some other way,
// emits IR that verifies cleanly and then does the wrong thing at run time.
//
// The domain therefore travels with every query, and the classification is
// reachable only for the domain that owns it (see CallLayout.Surge).
type ABIDomain uint8

const (
	// ABIDomainInvalid is the zero value and names no authority. It exists so
	// that a domain nobody set is rejected rather than silently read as the
	// generated contract.
	ABIDomainInvalid ABIDomain = iota
	// ABIDomainSurge marks a call between generated functions, which is the
	// only domain this contract classifies.
	ABIDomainSurge
	// ABIDomainRuntime marks a call into the native runtime, whose signatures
	// come from the hand-written declaration table.
	ABIDomainRuntime
	// ABIDomainForeign marks an explicit foreign C call, which follows its
	// declared target C ABI through dedicated lowering; a layout that lowering
	// cannot express is a compile-time diagnostic, never a fallback.
	ABIDomainForeign
)

func (d ABIDomain) String() string {
	switch d {
	case ABIDomainInvalid:
		return "invalid"
	case ABIDomainSurge:
		return "surge"
	case ABIDomainRuntime:
		return "runtime"
	case ABIDomainForeign:
		return "foreign"
	default:
		return fmt.Sprintf("ABIDomain(%d)", uint8(d))
	}
}

// governingAuthority names, for an error message, who classifies a call this
// contract refuses to classify.
func (d ABIDomain) governingAuthority() string {
	switch d {
	case ABIDomainRuntime:
		return "the hand-written runtime signature table governs it"
	case ABIDomainForeign:
		return "dedicated foreign C ABI lowering governs it, and a layout that lowering cannot express is a compile-time diagnostic"
	case ABIDomainInvalid:
		return "no calling authority was named"
	case ABIDomainSurge:
		return "the generated call contract governs it"
	default:
		return "no known calling authority"
	}
}

// ParamClass is how one argument reaches the callee.
type ParamClass uint8

const (
	// ParamGoverned is the zero value: this argument was not classified by the
	// generated contract. Every lowering must reject it, which is what keeps a
	// forgotten domain check from reading as a real class.
	ParamGoverned ParamClass = iota
	// ParamElidedZST marks a zero-sized argument. It keeps its lifecycle and
	// ordering meaning and occupies no argument position: no invented payload
	// byte, no dereferenceable dummy pointer.
	ParamElidedZST
	// ParamDirect marks an argument passed in its own concrete native
	// representation — scalars, handles, references and function values.
	ParamDirect
	// ParamByval marks a non-zero-sized composite argument passed by address
	// under the by-value contract with an explicit alignment. Exactly one copy
	// is made, by that lowering, from the caller's already-initialized value;
	// the caller does not pre-copy into a separate slot.
	ParamByval
)

func (c ParamClass) String() string {
	switch c {
	case ParamGoverned:
		return "governed-elsewhere"
	case ParamElidedZST:
		return "elided-zst"
	case ParamDirect:
		return "direct"
	case ParamByval:
		return "byval"
	default:
		return fmt.Sprintf("ParamClass(%d)", uint8(c))
	}
}

// RetClass is how a result reaches the caller.
type RetClass uint8

const (
	// RetGoverned is the zero value: this result was not classified by the
	// generated contract. See ParamGoverned for why the zero value is unusable.
	RetGoverned RetClass = iota
	// RetVoid marks a call that yields no payload — either no declared result
	// at all, or a zero-sized one that carries only lifecycle and ordering.
	RetVoid
	// RetDirect marks a result returned in its own concrete native
	// representation.
	RetDirect
	// RetSret marks a non-zero-sized composite result written into a hidden,
	// explicitly aligned caller-owned destination that the callee initializes
	// exactly once.
	RetSret
)

func (c RetClass) String() string {
	switch c {
	case RetGoverned:
		return "governed-elsewhere"
	case RetVoid:
		return "void"
	case RetDirect:
		return "direct"
	case RetSret:
		return "sret"
	default:
		return fmt.Sprintf("RetClass(%d)", uint8(c))
	}
}

// ParamLayout is one classified argument. Size and Align are the frozen
// physical facts of Type; no lowering may invent either.
type ParamLayout struct {
	Class ParamClass
	Type  types.TypeID
	Size  uint64
	Align uint64
}

// RetLayout is the classified result. When the callee declares no result at
// all, Type is types.NoTypeID and Size and Align are zero; when it declares a
// zero-sized result, Type names it and Align is that type's real alignment.
type RetLayout struct {
	Class RetClass
	Type  types.TypeID
	Size  uint64
	Align uint64
}

// SurgeABI is the generated call contract for one signature: what the caller
// must set up and what the callee promises to initialize.
type SurgeABI struct {
	Ret    RetLayout
	Params []ParamLayout
}

// CallLayout is the lowered call contract for one signature in one ABI domain.
//
// It is deliberately a pure function of the callee's TYPE and the domain, never
// of a function identity. A direct call resolves its signature from the
// definition it names while an indirect call has only the callee's type in
// hand; if the two could be keyed differently they could disagree about hidden
// destinations and by-value arguments for the same callee, which verifies
// cleanly and miscompiles. One classification, reached two ways, cannot.
//
// The fields are unexported so that the classification is unreachable for a
// domain that does not own it: a caller must go through Surge, which fails
// closed off-domain rather than handing back a zero value that could be
// mistaken for "no hidden destination, no by-value arguments".
type CallLayout struct {
	domain ABIDomain
	ret    RetLayout
	params []ParamLayout
}

// Domain reports which authority governs this call.
func (l CallLayout) Domain() ABIDomain {
	return l.domain
}

// Surge returns the generated call contract, or fails closed when another
// authority governs the call. The parameter list is copied, so a consumer can
// never reach back into a shared classification.
func (l CallLayout) Surge() (SurgeABI, error) {
	if l.domain != ABIDomainSurge {
		return SurgeABI{}, fmt.Errorf(
			"mir: call layout is not the generated contract's to state: %s",
			l.domain.governingAuthority(),
		)
	}
	return SurgeABI{Ret: l.ret, Params: append([]ParamLayout(nil), l.params...)}, nil
}

// ComputeCallLayout classifies a callee named by its function TYPE.
//
// Domains other than the generated one are answered with a marker carrying only
// the domain: no type is inspected and no classification is produced, which is
// what keeps the native runtime declarations and foreign calls off the hidden
// destination and by-value contract. Those callees may not even have a function
// type here, so fnType, typesIn and layouts are all allowed to be absent.
func ComputeCallLayout(
	typesIn *types.Interner,
	layouts *layout.Registry,
	fnType types.TypeID,
	domain ABIDomain,
) (CallLayout, error) {
	if domain == ABIDomainInvalid {
		return CallLayout{}, fmt.Errorf("mir: call layout requires an ABI domain")
	}
	if domain != ABIDomainSurge {
		return CallLayout{domain: domain}, nil
	}
	if typesIn == nil {
		return CallLayout{}, fmt.Errorf("mir: call layout requires a type interner")
	}
	resolved := resolveAliasAndOwn(typesIn, fnType)
	info, ok := typesIn.FnInfo(resolved)
	if !ok || info == nil {
		return CallLayout{}, fmt.Errorf("mir: type#%d is not a function type", fnType)
	}
	return ComputeCallLayoutForSignature(typesIn, layouts, info.Params, info.Result, domain)
}

// ComputeCallLayoutForSignature classifies a callee given its parameter and
// result types directly, for definition sites that carry a signature rather
// than an interned function type. It shares its whole classification with
// ComputeCallLayout, so the two cannot drift apart for one callee.
func ComputeCallLayoutForSignature(
	typesIn *types.Interner,
	layouts *layout.Registry,
	params []types.TypeID,
	result types.TypeID,
	domain ABIDomain,
) (CallLayout, error) {
	if domain == ABIDomainInvalid {
		return CallLayout{}, fmt.Errorf("mir: call layout requires an ABI domain")
	}
	if domain != ABIDomainSurge {
		return CallLayout{domain: domain}, nil
	}
	if typesIn == nil {
		return CallLayout{}, fmt.Errorf("mir: call layout requires a type interner")
	}
	if layouts == nil {
		return CallLayout{}, fmt.Errorf("mir: call layout requires finalized physical layouts")
	}
	ret, err := classifyResult(typesIn, layouts, result)
	if err != nil {
		return CallLayout{}, err
	}
	classified := make([]ParamLayout, 0, len(params))
	for i, param := range params {
		p, paramErr := classifyParam(typesIn, layouts, param)
		if paramErr != nil {
			return CallLayout{}, fmt.Errorf("mir: call layout parameter %d: %w", i, paramErr)
		}
		classified = append(classified, p)
	}
	return CallLayout{domain: ABIDomainSurge, ret: ret, params: classified}, nil
}

func classifyResult(
	typesIn *types.Interner,
	layouts *layout.Registry,
	result types.TypeID,
) (RetLayout, error) {
	if result == types.NoTypeID {
		return RetLayout{Class: RetVoid}, nil
	}
	size, align, err := physicalFacts(typesIn, layouts, result)
	if err != nil {
		return RetLayout{}, fmt.Errorf("mir: call layout result: %w", err)
	}
	out := RetLayout{Type: result, Size: size, Align: align}
	switch {
	case size == 0:
		// Nothing to hand back, whoever owns the storage. Unlike a parameter, a
		// result has no position to keep: a zero-sized value and no value at all
		// are the same returned function.
		out.Class = RetVoid
	case LivesInInlineStorage(typesIn, result):
		out.Class = RetSret
	default:
		out.Class = RetDirect
	}
	return out, nil
}

func classifyParam(
	typesIn *types.Interner,
	layouts *layout.Registry,
	param types.TypeID,
) (ParamLayout, error) {
	if param == types.NoTypeID {
		return ParamLayout{}, fmt.Errorf("parameter has no type")
	}
	size, align, err := physicalFacts(typesIn, layouts, param)
	if err != nil {
		return ParamLayout{}, err
	}
	out := ParamLayout{Type: param, Size: size, Align: align}
	switch {
	case !LivesInInlineStorage(typesIn, param):
		// A machine word, a handle, a borrow, or a suspension frame the runtime
		// owns. Each is carried as itself in one argument position.
		//
		// Asked BEFORE size, which is the opposite of the result ordering above
		// and is the difference between the two: a capture-free `blocking { }`
		// frame is zero bytes and still has to be passed, because the argument
		// is the runtime's handle to the frame rather than the frame's contents.
		out.Class = ParamDirect
	case size == 0:
		out.Class = ParamElidedZST
	default:
		out.Class = ParamByval
	}
	return out, nil
}

// physicalFacts reads the frozen size and alignment of one participating type.
//
// An unresolved layout is an error and never a fallback to passing the type
// directly: a composite that quietly became a direct argument would be an ABI
// the callee does not implement. The alias/own retry exists because a signature
// may name a type through a spelling the registry recorded under its canonical
// form; a miss on both spellings still fails.
func physicalFacts(
	typesIn *types.Interner,
	layouts *layout.Registry,
	id types.TypeID,
) (size, align uint64, err error) {
	facts, lookupErr := layouts.Require(id)
	if lookupErr != nil {
		resolved := resolveAliasAndOwn(typesIn, id)
		if resolved == id {
			return 0, 0, fmt.Errorf("unresolved layout for type#%d: %w", id, lookupErr)
		}
		resolvedFacts, resolvedErr := layouts.Require(resolved)
		if resolvedErr != nil {
			return 0, 0, fmt.Errorf("unresolved layout for type#%d: %w", id, resolvedErr)
		}
		facts = resolvedFacts
	}
	if facts.Align == 0 {
		return 0, 0, fmt.Errorf("type#%d has no proven alignment", id)
	}
	return facts.Size, facts.Align, nil
}

// resolveAliasAndOwn unwraps aliases and ownership so a type spelled through
// them answers exactly as the underlying type does.
//
// Both storage questions go through it — which representation a value has, and
// which argument class carries it — because a spelling that reached only one of
// them would answer the two questions differently for one type.
func resolveAliasAndOwn(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	for range aliasOwnResolveLimit {
		next := resolveAlias(typesIn, id)
		tt, ok := typesIn.Lookup(next)
		if !ok || tt.Kind != types.KindOwn || tt.Elem == types.NoTypeID {
			return next
		}
		id = tt.Elem
	}
	return id
}

// callLayoutKey identifies one classification. The function type is stored in
// the form resolveAliasAndOwn produced, so two spellings of one signature share
// an entry instead of racing to two.
type callLayoutKey struct {
	fn     types.TypeID
	domain ABIDomain
}

// CallLayoutTable answers call-contract queries for one finalized module and
// remembers each answer.
//
// It classifies on demand rather than enumerating signatures up front: every
// caller reaches the same classification through the same table, so there is no
// second path for a signature an enumeration would have missed, and a module
// nobody queries pays nothing and cannot fail.
//
// Answers are pure functions of their key, so the memo changes what the table
// costs and never what it says.
type CallLayoutTable struct {
	types   *types.Interner
	layouts *layout.Registry

	mu   sync.Mutex
	memo map[callLayoutKey]CallLayout
}

// NewCallLayoutTable binds a table to the frozen layouts it classifies against.
func NewCallLayoutTable(typesIn *types.Interner, layouts *layout.Registry) *CallLayoutTable {
	return &CallLayoutTable{types: typesIn, layouts: layouts}
}

// Of returns the call contract for a callee named by its function TYPE.
func (t *CallLayoutTable) Of(fnType types.TypeID, domain ABIDomain) (CallLayout, error) {
	if t == nil {
		return CallLayout{}, fmt.Errorf("mir: no finalized call layout table")
	}
	if domain == ABIDomainInvalid {
		return CallLayout{}, fmt.Errorf("mir: call layout requires an ABI domain")
	}
	if domain != ABIDomainSurge {
		return CallLayout{domain: domain}, nil
	}
	key := callLayoutKey{fn: resolveAliasAndOwn(t.types, fnType), domain: domain}
	t.mu.Lock()
	defer t.mu.Unlock()
	if cached, ok := t.memo[key]; ok {
		return cached, nil
	}
	computed, err := ComputeCallLayout(t.types, t.layouts, key.fn, domain)
	if err != nil {
		return CallLayout{}, err
	}
	if t.memo == nil {
		t.memo = make(map[callLayoutKey]CallLayout, 64)
	}
	t.memo[key] = computed
	return computed, nil
}

// OfSignature returns the call contract for a definition site, which carries
// its parameter and result types rather than an interned function type. It is
// asked once per definition and so is not memoized; the classification is the
// same one Of reaches.
func (t *CallLayoutTable) OfSignature(
	params []types.TypeID,
	result types.TypeID,
	domain ABIDomain,
) (CallLayout, error) {
	if t == nil {
		return CallLayout{}, fmt.Errorf("mir: no finalized call layout table")
	}
	return ComputeCallLayoutForSignature(t.types, t.layouts, params, result, domain)
}
