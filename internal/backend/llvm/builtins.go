package llvm

type builtinDecl struct {
	name   string
	ret    string
	params []string
}

func runtimeDecls() []builtinDecl {
	return []builtinDecl{
		{name: "rt_alloc", ret: "ptr", params: []string{"i64", "i64"}},
		{name: "rt_free", ret: "void", params: []string{"ptr", "i64", "i64"}},
		{name: "rt_realloc", ret: "ptr", params: []string{"ptr", "i64", "i64", "i64"}},
		{name: "rt_memcpy", ret: "void", params: []string{"ptr", "ptr", "i64"}},
		{name: "rt_memmove", ret: "void", params: []string{"ptr", "ptr", "i64"}},
		{name: "rt_write_stdout", ret: "i64", params: []string{"ptr", "i64"}},
		{name: "rt_write_stderr", ret: "i64", params: []string{"ptr", "i64"}},
		{name: "rt_string_from_bytes", ret: "ptr", params: []string{"ptr", "i64"}},
		{name: "rt_string_ptr", ret: "ptr", params: []string{"ptr"}},
		{name: "rt_string_len", ret: "i64", params: []string{"ptr"}},
		{name: "rt_string_len_bytes", ret: "i64", params: []string{"ptr"}},
		{name: "rt_string_concat", ret: "ptr", params: []string{"ptr", "ptr"}},
		{name: "rt_string_eq", ret: "i1", params: []string{"ptr", "ptr"}},
		{name: "rt_exit", ret: "void", params: []string{"i64"}},
		{name: "rt_panic", ret: "void", params: []string{"ptr", "i64"}},
	}
}

func runtimeSigMap() map[string]funcSig {
	decls := runtimeDecls()
	m := make(map[string]funcSig, len(decls))
	for _, d := range decls {
		m[d.name] = funcSig{ret: d.ret, params: d.params}
	}
	return m
}
