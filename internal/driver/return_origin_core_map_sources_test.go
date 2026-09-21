package driver

// The frozen witness sources of TestAnalyzeCoreMapOrigins and the owning-unit census rows
// P1c-1 removes (23) and leaves (24), as captured at the D2 tip ed57f3ec.

const coreMapM1 = `fn fill(m: &mut Map<string, &string>, value: &string) -> nothing {
    let _ = m.insert("k", value);
    return nothing;
}
fn outer(value: &string) -> nothing {
    let mut m: Map<string, &string> = Map::<string, &string>.new();
    {
        let s: string = "local";
        fill(&mut m, &s);
    }
    return nothing;
}
`

const coreMapM1Digest = "6a3f5777cefddd421b7a0d5c54070744a69ce0ad62e15769ff92d3032177309e"

const coreMapM2 = `pragma module::dep;
fn after_remove(m: &mut Map<string, &string>, key: &string, value: &string) -> Option<&string> {
    let _ = m.insert("k", value);
    let _ = m.remove(key);
    return m.remove(key);
}
`

const coreMapM2Digest = "c3b923fff33f4fc38f8e0466eb77e56355e3881faf4d5b17746c77b54d04c5c9"

const coreMapM3 = `pragma module::dep;
fn names<V>(m: &Map<string, V>) -> string[] {
    return m.keys();
}
fn use_names(m: &Map<string, &string>) -> string[] {
    return names::<&string>(m);
}
`

const coreMapM3Digest = "d6f291fe42bdb1f1c11159194911e7bd79ced7bdac4c8882fb5c5aa4273577bc"

const coreMapM4 = `pragma module::dep, no_std;
@intrinsic fn rt_map_len<K, V>(m: &Map<K, V>) -> uint;
fn size(m: &Map<string, &string>) -> uint {
    return rt_map_len::<string, &string>(m);
}
`

const coreMapM4Digest = "499379507fa5e4a989181acfcebf16c0b3ee47c60b117c8db04dfa4223cb045c"

const coreMapM5 = `fn wrapm<V>(v: V) -> Map<string, V> {
    let mut m: Map<string, V> = rt_map_new::<string, V>();
    let _ = rt_map_insert(&mut m, "k", v);
    return m;
}
fn leak() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut mm: Map<string, uint64[]> = wrapm::<uint64[]>(xs[[1..3]]);
    return rt_map_remove(&mut mm, &"k");
}
fn leak_checked() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut mm: Map<string, uint64[]> = wrapm::<uint64[]>(xs[[1..3]]);
    return mm.remove(&"k");
}
fn leak_let() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut mm: Map<string, uint64[]> = wrapm::<uint64[]>(xs[[1..3]]);
    let v = rt_map_remove(&mut mm, &"k");
    return v;
}
`

const coreMapM5Digest = "04d0cc07e11337ab2fab1b1d5b50603791f3c2ea723d8b1f21d8760a33e6a2fe"

const coreMapM6 = `pragma module::dep;
fn put_get<V>(m: &mut Map<string, V>, v: V, key: &string) -> Option<V> {
    let _ = rt_map_insert(m, "k", v);
    let _ = rt_map_remove(m, key);
    return rt_map_remove(m, key);
}
fn mix<V>(m: &mut Map<string, V>, xs: &mut Array<V>, v: V) -> Option<V> {
    let _ = rt_map_insert(m, "k", v);
    return rt_array_pop(xs);
}
`

const coreMapM6Digest = "240054992ab8f951b343dc0e4f008316de3ee7cccc026f50015ccc9cd1f9ba37"

const coreMapM7 = `pragma module::dep;
fn put_inner(xs: &uint64[4]) -> nothing {
    let mut outer: Array<Map<string, uint64[]>> = [];
    let _ = rt_map_insert(&mut outer[0], "k", xs[[1..3]]);
    return nothing;
}
`

const coreMapM7Digest = "04dc167a1f0917831bb24567b268390b5e55721cf92e911e89d36b6d1c38a073"

const coreMapM8 = `pragma module::dep;
fn wrapm<V>(v: V) -> Map<string, V> {
    let mut m: Map<string, V> = rt_map_new::<string, V>();
    let _ = rt_map_insert(&mut m, "k", v);
    return m;
}
fn wrap<T>(x: T) -> Array<T> {
    let mut out: Array<T> = [];
    out.push(x);
    return out;
}
fn remove_inner() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut outer: Array<Map<string, uint64[]>> = wrap::<Map<string, uint64[]>>(wrapm::<uint64[]>(xs[[1..3]]));
    let v = rt_map_remove(&mut outer[0], &"k");
    return v;
}
`

const coreMapM8Digest = "f699fa8f8ad4c6b6cc69305f00bf954e2932e29f76585984740c9fc83c88a299"

const coreMapM9 = `pragma module::dep;
fn lend_inner(key: &string) -> nothing {
    let mut outer: Array<Map<string, uint64[]>> = [];
    let _ = rt_map_get_mut(&mut outer[0], key);
    return nothing;
}
`

const coreMapM9Digest = "fa1d731b16cf5a4f665c5dfc679d60fcc859b0666d9dfcde76f3d780ae373f3d"

const coreMapM10 = `pragma module::dep;
fn remove_option(key: &string) -> nothing {
    let mut outer: Array<Map<string, Option<uint64[]>>> = [];
    let _ = rt_map_remove(&mut outer[0], key);
    return nothing;
}
`

const coreMapM10Digest = "152e0821bfd4fd55ccf7773aa827eb37e33fe6603f94b4cf5c67b0a0e9d9e1f5"

const coreMapUnitDigest = "34ff14d90f99b70f934fb99a4c02dc73fc20d77e574bdbe87e0b817048ee0c9e"

const coreIntrinsicsUnitDigest = "ef9d4c657599262b678e9f59640e545936bb3d87ea70e173d145f3abccdfe060"

// coreMapCensusRow is one owning-unit census triple; text is the core bytes at the span.
type coreMapCensusRow struct {
	key        string
	start, end int
	reason     string
	text       string
}

var coreMapRemovedRows = []coreMapCensusRow{
	{"core/intrinsics.sg", 5978, 5988, "opaque result borrowed-state classification is unsupported", "rt_map_new"},
	{"core/intrinsics.sg", 6496, 6507, "opaque result borrowed-state classification is unsupported", "rt_map_keys"},
	{"core/map.sg", 107, 119, "function result contains an unproved source", "-> Map<K, V>"},
	{"core/map.sg", 130, 158, "outgoing reference has unresolved or captured provenance", "return rt_map_new::<K, V>();"},
	{"core/map.sg", 137, 157, "callee returned an unproved source", "rt_map_new::<K, V>()"},
	{"core/map.sg", 137, 157, "opaque result borrowed-state classification is unsupported", "rt_map_new::<K, V>()"},
	{"core/map.sg", 227, 243, "opaque call may change reference-bearing or callable contents", "rt_map_len(self)"},
	{"core/map.sg", 312, 328, "opaque call may change reference-bearing or callable contents", "rt_map_len(self)"},
	{"core/map.sg", 371, 377, "function result contains an unproved source", "-> K[]"},
	{"core/map.sg", 388, 413, "outgoing reference has unresolved or captured provenance", "return rt_map_keys(self);"},
	{"core/map.sg", 395, 412, "callee returned an unproved source", "rt_map_keys(self)"},
	{"core/map.sg", 395, 412, "opaque call may change reference-bearing or callable contents", "rt_map_keys(self)"},
	{"core/map.sg", 395, 412, "opaque result borrowed-state classification is unsupported", "rt_map_keys(self)"},
	{"core/map.sg", 493, 519, "opaque call may change reference-bearing or callable contents", "rt_map_contains(self, key)"},
	{"core/map.sg", 605, 630, "opaque call may change reference-bearing or callable contents", "rt_map_get_ref(self, key)"},
	{"core/map.sg", 724, 749, "mutable argument may replace reference-bearing contents", "rt_map_get_mut(self, key)"},
	{"core/map.sg", 724, 749, "opaque call may change reference-bearing or callable contents", "rt_map_get_mut(self, key)"},
	{"core/map.sg", 846, 877, "mutable argument may replace reference-bearing contents", "rt_map_insert(self, key, value)"},
	{"core/map.sg", 846, 877, "opaque call may change reference-bearing or callable contents", "rt_map_insert(self, key, value)"},
	{"core/map.sg", 965, 989, "mutable argument may replace reference-bearing contents", "rt_map_remove(self, key)"},
	{"core/map.sg", 965, 989, "opaque call may change reference-bearing or callable contents", "rt_map_remove(self, key)"},
	{"core/map.sg", 1311, 1342, "mutable argument may replace reference-bearing contents", "rt_map_insert(self, key, value)"},
	{"core/map.sg", 1311, 1342, "opaque call may change reference-bearing or callable contents", "rt_map_insert(self, key, value)"},
}

var coreMapRemainingRows = []coreMapCensusRow{
	{"core/array.sg", 1971, 1984, "parameter requires concrete type or callable provenance", ""},
	{"core/array.sg", 2068, 2069, "outgoing reference has unresolved or captured provenance", ""},
	{"core/array.sg", 2090, 2360, "outgoing reference has unresolved or captured provenance", ""},
	{"core/array.sg", 2130, 2134, "outgoing reference has unresolved or captured provenance", ""},
	{"core/array.sg", 2201, 2261, "outgoing reference has unresolved or captured provenance", ""},
	{"core/array.sg", 2261, 2261, "outgoing reference has unresolved or captured provenance", ""},
	{"core/array.sg", 2311, 2317, "outgoing reference has unresolved or captured provenance", ""},
	{"core/array.sg", 2369, 2380, "outgoing reference has unresolved or captured provenance", ""},
	{"core/array.sg", 2893, 2904, "function result contains an unproved source", ""},
	{"core/array.sg", 2915, 2930, "outgoing reference has unresolved or captured provenance", ""},
	{"core/array.sg", 2922, 2929, "index requires a non-scalar index transfer", ""},
	{"core/intrinsics.sg", 6744, 6760, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 6833, 6856, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 6919, 6938, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 6999, 7016, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 9752, 9757, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 10046, 10056, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 10154, 10159, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 37316, 37321, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 37475, 37482, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 37635, 37642, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 37771, 37776, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 37962, 37969, "opaque result borrowed-state classification is unsupported", ""},
	{"core/intrinsics.sg", 38138, 38145, "opaque result borrowed-state classification is unsupported", ""},
}
