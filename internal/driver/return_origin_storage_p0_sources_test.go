package driver

import "surge/internal/ast"

// These are admission probes, not claims that the proposed P1 storage domain exists.
type storageP0Case struct {
	name    string
	source  string
	targets []storageP0Target
}

type storageP0Target struct {
	unit     string // Empty selects the original root source.
	text     string
	kind     ast.ExprKind
	count    int
	selected string // Required canonical selected declaration, when applicable.
}

var storageP0Cases = []storageP0Case{
	{
		name: "p05_default_writes",
		source: `fn empty() -> Array<Option<&string>> { return []; }
fn zero() -> Option<&string>[0] { return default::<Option<&string>[0]>(); }
fn partial(value: &string, write: bool) -> Option<&string>[2] {
    let mut out: Option<&string>[2] = default::<Option<&string>[2]>();
    if write { out[0] = Some::<&string>(value); }
    return out;
}
fn full(value: &string) -> Option<&string>[2] {
    let mut out: Option<&string>[2] = default::<Option<&string>[2]>();
    let mut i: int = 0;
    while i < 2 { out[i] = Some::<&string>(value); i = i + 1; }
    return out;
}
`,
		targets: []storageP0Target{
			{"", "[]", ast.ExprArray, 1, ""},
			{"", "default::<Option<&string>[0]>()", ast.ExprCall, 1, "default"},
			{"", "default::<Option<&string>[2]>()", ast.ExprCall, 2, "default"},
			{"", "out[0] = Some::<&string>(value)", ast.ExprBinary, 1, ""},
			{"", "out[i] = Some::<&string>(value)", ast.ExprBinary, 1, ""},
		},
	},
	{
		name: "p06_core_array_paths",
		source: `fn probe(value: uint64) -> uint64[] {
    let mut dst: uint64[] = [];
    let src: uint64[2] = [value, value];
    let extra: uint64[] = src.to_array();
    dst.extend(&extra);
    dst.reverse_in_place();
    return dst;
}
`,
		targets: []storageP0Target{
			{"", "src.to_array()", ast.ExprCall, 1, "to_array"},
			{"", "dst.extend(&extra)", ast.ExprCall, 1, "extend"},
			{"", "dst.reverse_in_place()", ast.ExprCall, 1, "reverse_in_place"},
			{"core/array.sg", "clone(other[i])", ast.ExprCall, 1, ""},
			{"core/array.sg", "clone(self[i])", ast.ExprCall, 2, ""},
			{"core/array.sg", "clone(self[j])", ast.ExprCall, 1, ""},
			{"core/array.sg", "self[i] = clone(self[j])", ast.ExprBinary, 1, ""},
			{"core/array.sg", "self[j] = tmp", ast.ExprBinary, 1, ""},
			{"core/array.sg", "out[i] = clone(&value)", ast.ExprBinary, 1, ""},
			{"core/array.sg", "self[r]", ast.ExprIndex, 1, "__index"},
			{"core/map.sg", "rt_map_keys(self)", ast.ExprCall, 1, "rt_map_keys"},
		},
	},
	{
		name: "p06_indexed_external",
		source: `fn read<T>(xs: &Array<T>) -> T { return clone(xs[0]); }
fn probe(value: &string) -> &string {
    let xs: Array<&string> = [value];
    return read::<&string>(&xs);
}
`,
		targets: []storageP0Target{
			{"", "xs[0]", ast.ExprIndex, 1, "__index"},
			{"", "clone(xs[0])", ast.ExprCall, 1, ""},
			{"", "read::<&string>(&xs)", ast.ExprCall, 1, "read"},
		},
	},
	{
		name: "p06_indexed_local",
		source: `fn read<T>(xs: &Array<T>) -> T { return clone(xs[0]); }
fn probe() -> &string {
    let owned: string = "local";
    let xs: Array<&string> = [&owned];
    return read::<&string>(&xs);
}
`,
		targets: []storageP0Target{
			{"", "xs[0]", ast.ExprIndex, 1, "__index"},
			{"", "clone(xs[0])", ast.ExprCall, 1, ""},
			{"", "read::<&string>(&xs)", ast.ExprCall, 1, "read"},
		},
	},
	{
		name: "p07_view_rebind",
		source: `fn probe(value: uint64, replacement: uint64) -> uint64[] {
    let mut base: uint64[] = [value];
    let mut view: uint64[] = base[[0..1]];
    base = [replacement];
    let old: uint64 = clone(view[0]);
    view[0] = old;
    return view;
}
`,
		targets: []storageP0Target{
			{"", "base[[0..1]]", ast.ExprIndex, 1, "__index"},
			{"", "base = [replacement]", ast.ExprBinary, 1, ""},
			{"", "clone(view[0])", ast.ExprCall, 1, ""},
			{"", "view[0] = old", ast.ExprBinary, 1, ""},
		},
	},
	{
		name: "p07_repeated_site",
		source: `fn probe(first: uint64, later: uint64) -> uint64[] {
    let mut saved: uint64[] = [];
    let mut i: int = 0;
    while i < 2 {
        let current: uint64[] = [first];
        let mut view: uint64[] = current[[0..1]];
        if i == 0 { saved = view; } else { view[0] = later; }
        i = i + 1;
    }
    return saved;
}
`,
		targets: []storageP0Target{
			{"", "[first]", ast.ExprArray, 1, ""},
			{"", "current[[0..1]]", ast.ExprIndex, 1, "__index"},
			{"", "saved = view", ast.ExprBinary, 1, ""},
			{"", "view[0] = later", ast.ExprBinary, 1, ""},
		},
	},
	{
		name: "p08_read_write_order",
		source: `fn read_old(dst: &mut &string, replacement: &string) -> &string {
    let old: &string = *dst;
    *dst = replacement;
    return old;
}
fn read_new(dst: &mut &string, replacement: &string) -> &string {
    *dst = replacement;
    return *dst;
}
fn returned_alias(dst: &mut &string, replacement: &string) -> &mut &string {
    *dst = replacement;
    return dst;
}
fn probe(value: &string, replacement: &string) -> &string {
    let mut cell: &string = value;
    let alias: &mut &string = returned_alias(&mut cell, replacement);
    *alias = value;
    return *alias;
}
`,
		targets: []storageP0Target{
			{"", "*dst = replacement", ast.ExprBinary, 3, ""},
			{"", "returned_alias(&mut cell, replacement)", ast.ExprCall, 1, "returned_alias"},
			{"", "*alias = value", ast.ExprBinary, 1, ""},
		},
	},
	{
		name: "p09_alias_coalescence",
		source: `fn write_then_read<T, U>(dst: &mut Array<T>, src: Array<U>, replacement: T) -> U {
    dst[0] = replacement;
    return clone(src[0]);
}
fn probe(value: uint64, replacement: uint64) -> uint64 {
    let mut base: uint64[] = [value];
    let view: uint64[] = base[[0..1]];
    return write_then_read::<uint64, uint64>(&mut base, view, replacement);
}
`,
		targets: []storageP0Target{
			{"", "dst[0] = replacement", ast.ExprBinary, 1, ""},
			{"", "clone(src[0])", ast.ExprCall, 1, ""},
			{"", "base[[0..1]]", ast.ExprIndex, 1, "__index"},
			{"", "write_then_read::<uint64, uint64>(&mut base, view, replacement)", ast.ExprCall, 1, "write_then_read"},
		},
	},
	{
		name: "p10_view_cursor_lifetime",
		source: `fn through_ref(xs: &uint64[4]) -> uint64[] { return xs[[1..3]]; }
fn dynamic() -> uint64[] {
    let mut xs: uint64[] = [11:uint64, 22:uint64, 33:uint64, 44:uint64];
    return xs[[1..3]];
}
fn cursor_ref(xs: &uint64[]) -> Range<uint64> { return xs.__range(); }
fn probe() -> Option<uint64> {
    let xs: uint64[4] = [11:uint64, 22:uint64, 33:uint64, 44:uint64];
    let local = xs[[1..3]];
    let _ = local[0];
    let mut cursor: Range<uint64> = xs.__range();
    return cursor.next();
}
`,
		targets: []storageP0Target{
			{"", "xs[[1..3]]", ast.ExprIndex, 3, "__index"},
			{"", "xs.__range()", ast.ExprCall, 2, "__range"},
			{"", "cursor.next()", ast.ExprCall, 1, "next"},
		},
	},
	{
		name: "p11_external_key_local_value",
		source: `fn probe(key: &string) -> Array<&string> {
    let local: string = "value";
    let mut m: Map<&string, &string> = Map::<&string, &string>.new();
    let _ = m.insert(key, &local);
    return m.keys();
}
`,
		targets: []storageP0Target{
			{"", "m.insert(key, &local)", ast.ExprCall, 1, "insert"},
			{"", "m.keys()", ast.ExprCall, 1, "keys"},
			{"core/map.sg", "rt_map_keys(self)", ast.ExprCall, 1, "rt_map_keys"},
		},
	},
	{
		name: "p11_local_key_external_value",
		source: `fn probe(value: &string) -> Array<&string> {
    let local: string = "key";
    let mut m: Map<&string, &string> = Map::<&string, &string>.new();
    let _ = m.insert(&local, value);
    return m.keys();
}
`,
		targets: []storageP0Target{
			{"", "m.insert(&local, value)", ast.ExprCall, 1, "insert"},
			{"", "m.keys()", ast.ExprCall, 1, "keys"},
		},
	},
	{
		name: "p11_replace_effects",
		source: `fn read_old(m: &mut Map<string, &string>, key: string, value: &string) -> Option<&string> {
    let old: Option<&string> = m.insert(clone(&key), value);
    let _ = m.contains(&key);
    let _ = m.length();
    let _ = m.get_ref(&key);
    let _ = m.get_mut(&key);
    let _ = m.remove(&key);
    return old;
}
fn read_new(m: &mut Map<string, &string>, key: string, value: &string) -> Option<&string> {
    let _ = m.insert(clone(&key), value);
    return m.remove(&key);
}
fn safe_keys(value: string) -> string[] {
    let mut m: Map<string, string> = Map::<string, string>.new();
    let _ = m.insert("key", value);
    return m.keys();
}
`,
		targets: []storageP0Target{
			{"", "m.insert(clone(&key), value)", ast.ExprCall, 2, "insert"},
			{"", "m.get_ref(&key)", ast.ExprCall, 1, "get_ref"},
			{"", "m.get_mut(&key)", ast.ExprCall, 1, "get_mut"},
			{"", "m.remove(&key)", ast.ExprCall, 2, "remove"},
			{"", "m.keys()", ast.ExprCall, 1, "keys"},
			{"core/map.sg", "rt_map_keys(self)", ast.ExprCall, 1, "rt_map_keys"},
			{"core/map.sg", "rt_map_insert(self, key, value)", ast.ExprCall, 2, "rt_map_insert"},
			{"core/map.sg", "rt_map_remove(self, key)", ast.ExprCall, 1, "rt_map_remove"},
			{"core/map.sg", "rt_map_get_ref(self, key)", ast.ExprCall, 1, "rt_map_get_ref"},
			{"core/map.sg", "rt_map_get_mut(self, key)", ast.ExprCall, 1, "rt_map_get_mut"},
			{"core/map.sg", "rt_map_len(self)", ast.ExprCall, 2, "rt_map_len"},
			{"core/map.sg", "rt_map_contains(self, key)", ast.ExprCall, 1, "rt_map_contains"},
		},
	},
}
