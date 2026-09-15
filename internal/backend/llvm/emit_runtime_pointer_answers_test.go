package llvm

// runtimeAnswerClass says what a pointer-answering runtime entry point does when
// the allocator refuses it.
type runtimeAnswerClass int

const (
	// refusalIsReported: the entry point stops the process itself. Generated
	// code never sees the refusal.
	refusalIsReported runtimeAnswerClass = iota
	// nullIsNotARefusal: it can answer NULL, but never because it was refused.
	nullIsNotARefusal
	// refusalIsTested: it answers NULL on refusal and the generated code tests
	// that answer before storing through it.
	refusalIsTested
	// refusalIsUntested: it answers NULL on refusal and the generated code does
	// NOT test it. A live hole, recorded here so this census reports it instead
	// of reporting coverage it does not have.
	refusalIsUntested
	// refusalIsSwallowed: the refusal never reaches the generated code AS a
	// refusal — the entry point drops it inside itself and answers a NUMBER. No
	// test the emitter could write would see this one: the answer is a legal
	// value, not a null. It is a class of its own because it is neither
	// reported nor testable, and calling it either would be a false row.
	refusalIsSwallowed
)

type runtimeAnswer struct {
	class  runtimeAnswerClass
	reason string
}

func classified(class runtimeAnswerClass, reason string, names ...string) map[string]runtimeAnswer {
	out := make(map[string]runtimeAnswer, len(names))
	for _, name := range names {
		out[name] = runtimeAnswer{class: class, reason: reason}
	}
	return out
}

// runtimePointerAnswers classifies every runtime entry point that hands a
// pointer to generated code. The reasons are what a reader needs to disagree
// with the classification; each was read out of the named runtime source.
func runtimePointerAnswers() map[string]runtimeAnswer {
	groups := []map[string]runtimeAnswer{
		classified(refusalIsTested,
			"emitCheckedAlloc writes it and tests the answer; the memory intrinsic is excused per file, "+
				"because there the nullable answer is the language's own",
			"rt_alloc"),
		classified(refusalIsTested,
			"emitCheckedRealloc writes it and reports BEFORE the header records the answer, "+
				"because a refused reallocation releases nothing (runtime/native/rt_alloc.c: rt_realloc)",
			"rt_realloc"),
		classified(refusalIsTested,
			"emitCheckedFrameAlloc writes it and tests the answer; it reaches rt_alloc at the width "+
				"its descriptor states and reports nothing itself, because the sentence names the TYPE "+
				"and only the caller has one (runtime/native/rt_frame.c: rt_frame_alloc)",
			"rt_frame_alloc"),
		classified(refusalIsTested,
			"emitCheckedRangeNew for the bounded form and emitRuntimeAnswerTest for the open-ended ones, "+
				"which are reached as ordinary calls to a runtime symbol; all five share alloc_range "+
				"(runtime/native/rt_range.c). rt_range_int_new is rt_range_bounds_new with the `int` "+
				"bound kind, which is the kind the type checker holds a range LITERAL's bounds to; the "+
				"operator spelling `a..b` is generic over its bound type and reaches the general one",
			"rt_range_bounds_new",
			"rt_range_int_new", "rt_range_int_from_start", "rt_range_int_to_end", "rt_range_int_full"),

		classified(refusalIsReported,
			"the limb block is taken with an error out-parameter, and a refused one is reported as the "+
				"numeric size limit through bignum_panic_err (runtime/native/rt_bignum_int.c: bi_alloc, "+
				"rt_bignum_uint_core.c: bu_alloc, rt_bignum_panic.c); the NULL these answer beside it is "+
				"the tagged encoding of zero, not a refusal",
			"rt_bigint_from_i64", "rt_bigint_from_literal", "rt_bigint_from_u64",
			"rt_biguint_from_literal", "rt_biguint_from_u64", "rt_biguint_to_bigint",
			"rt_bigfloat_abs", "rt_bigfloat_add", "rt_bigfloat_clone", "rt_bigfloat_div",
			"rt_bigfloat_unshare",
			"rt_bigfloat_from_f64", "rt_bigfloat_from_i64", "rt_bigfloat_from_literal",
			"rt_bigfloat_from_u64", "rt_bigfloat_mod", "rt_bigfloat_mul", "rt_bigfloat_neg",
			"rt_bigfloat_sub", "rt_bigfloat_to_bigint", "rt_bigfloat_to_biguint"),
		classified(refusalIsReported,
			"clone and shared unshare pass an error destination to bi_clone/bu_clone and report "+
				"a refused allocation through bignum_panic_err (runtime/native/rt_bignum_lifecycle.c); "+
				"NULL is zero, inline fixnums own no block, and unique unshare allocates nothing",
			"rt_bigint_clone", "rt_bigint_unshare", "rt_biguint_clone", "rt_biguint_unshare"),
		classified(refusalIsSwallowed,
			"promotes a tagged operand with NO error out-parameter — bi_promote calls "+
				"bi_from_i64(fixi_value(w), NULL) and bu_promote calls bu_from_u64(fixu_value(w), NULL) "+
				"(runtime/native/rt_bignum_api.c: bi_promote, bu_promote), and bi_alloc/bu_alloc only "+
				"record BN_ERR_MAX_LIMBS when they are given somewhere to record it. A refused promotion "+
				"therefore yields a NULL operand that is indistinguishable from the tagged zero, and "+
				"bi_add(NULL, b, &err) answers bi_clone(b) with err still BN_OK: `a + b` returns `b`. "+
				"This is ordinary int arithmetic, not a wide-number corner. The repair is the error "+
				"out-parameter these two helpers do not pass, and it belongs to the bignum lane",
			"rt_bigint_abs", "rt_bigint_add", "rt_bigint_bit_and", "rt_bigint_bit_or", "rt_bigint_bit_xor",
			"rt_bigint_div", "rt_bigint_mod", "rt_bigint_mul", "rt_bigint_neg", "rt_bigint_shl",
			"rt_bigint_shr", "rt_bigint_sub", "rt_bigint_to_bigfloat",
			"rt_biguint_add", "rt_biguint_bit_and", "rt_biguint_bit_or", "rt_biguint_bit_xor",
			"rt_biguint_div", "rt_biguint_mod", "rt_biguint_mul", "rt_biguint_shl", "rt_biguint_shr",
			"rt_biguint_sub", "rt_biguint_to_bigfloat"),
		classified(refusalIsSwallowed,
			"clones the magnitude with no error out-parameter — bu_clone(bi_as_uint(src), NULL) "+
				"(runtime/native/rt_bignum_api.c: rt_bigint_to_biguint) — so a refused clone is handed to "+
				"bu_finish as NULL and the conversion answers zero for a number that was not zero",
			"rt_bigint_to_biguint"),
		classified(refusalIsReported,
			"reports through its own panic before returning: array_panic / concat_panic / map_panic "+
				"(runtime/native/rt_array.c, rt_array_concat.c, rt_map.c)",
			"rt_array_concat", "rt_array_slice", "rt_array_slice_fixed", "rt_map_new", "rt_map_keys"),
		classified(refusalIsReported,
			"panic_msg on a refused task, job or channel block (runtime/native/rt_async_task.c, "+
				"rt_async_blocking.c, rt_async_channel.c); the NULL beside it answers an "+
				"executor that ensure_exec returns a static for and never fails to give",
			"__task_create", "__task_create_affine", "__task_state", "checkpoint", "rt_sleep",
			"rt_blocking_submit", "rt_channel_new"),
		classified(refusalIsReported,
			"tests its own answer and reports (runtime/native/rt_io.c: rt_readline, rt_term.c: rt_term_size)",
			"rt_readline", "rt_term_size"),
		classified(refusalIsReported,
			"a refused string block stops the process in string_alloc_or_report (runtime/native/rt_string.c); "+
				"every one of these reaches its storage through it",
			"rt_string_from_bytes", "rt_string_concat", "rt_string_repeat", "rt_string_clone",
			"rt_string_slice", "rt_string_from_int", "rt_string_from_uint", "rt_string_from_float",
			"rt_string_from_bigint", "rt_string_from_biguint", "rt_string_from_bigfloat",
			"rt_stdin_read_all"),

		classified(nullIsNotARefusal,
			"borrows the bytes of a live string and allocates nothing; its NULL answers a handle that is "+
				"not there (runtime/native/rt_string.c: rt_string_ptr)",
			"rt_string_ptr"),
		classified(nullIsNotARefusal,
			"adds a handle reference and allocates nothing; its NULL answers a task handle that is not "+
				"there (runtime/native/rt_async_task.c: rt_task_clone)",
			"rt_task_clone"),
		classified(nullIsNotARefusal,
			"no definition exists in runtime/native, so no call to it answers anything: a native program "+
				"that reaches this lowering does not link. Recorded rather than left blank",
			"rt_string_from_utf16"),
		classified(nullIsNotARefusal,
			"answers the address of a static hash generated into the runtime "+
				"(internal/abimanifest/render_c.go); it allocates nothing",
			"rt_typed_carrier_abi_manifest_hash"),

		classified(refusalIsReported,
			"a refused FsResult block stops the process in rt_tag_alloc_or_report "+
				"(runtime/native/rt_fs_result.c: fs_make_error, fs_make_success_*, through "+
				"FS_RESULT_ALLOC); the generated code stores the answer into the FsResult slot "+
				"untested, and since Wave F F2 that answer is never NULL",
			"rt_fs_open", "rt_fs_read", "rt_fs_write", "rt_fs_seek", "rt_fs_close", "rt_fs_flush",
			"rt_fs_read_file", "rt_fs_write_file", "rt_fs_cwd", "rt_fs_metadata", "rt_fs_mkdir",
			"rt_fs_read_dir", "rt_fs_remove_dir", "rt_fs_remove_file", "rt_fs_file_metadata",
			"rt_fs_file_name", "rt_fs_file_type"),
		classified(refusalIsReported,
			"a refused NetResult block, or the refused byte-array header behind one, stops the "+
				"process in rt_tag_alloc_or_report / rt_alloc_or_report (runtime/native/"+
				"rt_net_result.c: net_make_error, net_make_success_*, through NET_RESULT_ALLOC); "+
				"the generated code stores the answer untested, and since Wave F F2 it is never NULL",
			"rt_net_accept", "rt_net_connect", "rt_net_listen", "rt_net_read", "rt_net_read_bytes",
			"rt_net_write", "rt_net_write_bytes", "rt_net_close_conn", "rt_net_close_listener"),
		classified(refusalIsReported,
			"a refused block stops the process in rt_alloc_or_report / rt_tag_alloc_or_report "+
				"(runtime/native/rt_entropy.c: entropy_make_*, rt_io.c: rt_argv, rt_alloc.c: "+
				"rt_heap_stats, which also reports an accounting snapshot it cannot take); the "+
				"generated code stores the answer untested, and since Wave F F2 it is never NULL",
			"rt_entropy_bytes", "rt_argv", "rt_heap_stats"),
		classified(nullIsNotARefusal,
			"a refused view block stops the process in rt_alloc_or_report (runtime/native/rt_string.c: "+
				"rt_string_bytes_view); the NULL it still answers is for a string handle that is not "+
				"there, which is the language's own answer and not a refusal",
			"rt_string_bytes_view"),
		classified(refusalIsReported,
			"a refused event block, at any of the levels an event is built from, stops the process "+
				"in rt_tag_alloc_or_report / rt_alloc_or_report (runtime/native/rt_term.c: "+
				"term_make_key, term_make_key_event, term_make_event_key, term_make_event_resize, "+
				"term_make_event_eof, through TERM_EVENT_ALLOC); the generated code stores the answer "+
				"untested, and since Wave F F2 it is never NULL",
			"rt_term_read_event"),
	}
	out := map[string]runtimeAnswer{}
	for _, group := range groups {
		for name, answer := range group {
			out[name] = answer
		}
	}
	return out
}
