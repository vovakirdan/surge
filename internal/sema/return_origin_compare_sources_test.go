package sema

type returnOriginCompareFixture struct {
	name, text, digest, owner     string
	slots                         []uint32
	checkSlots, complete, unknown bool
}

// Source bytes match the reviewed 14-case inventory; order freezes the roster.
func returnOriginCompareFixtures() []returnOriginCompareFixture {
	return []returnOriginCompareFixture{
		{name: "mixed_returns", digest: "3d610ba60b8b6c02cb2bd852acd35a671927512d2636d9027b6639e63ee1a43c", slots: []uint32{1, 2}, checkSlots: true, complete: true, text: `pragma no_std;
fn probe(flag: bool, a: &string, b: &string) -> &string {
    return compare flag { true => { return a; } false => b; };
}
`},
		{name: "subject_effect", digest: "82b7129dd9f035a19abe11211978e4e3f5a61a26d61b78c2b1f2cae04756e187", slots: []uint32{2}, checkSlots: true, complete: true, text: `pragma no_std;
fn probe(flag: bool, a: &string, b: &string) -> &string {
    let mut alias: &string = a;
    return compare { let _ = nothing; alias = b; ret flag; } { true => alias; false => alias; };
}
`},
		{name: "guard_fallthrough", digest: "055d46983acf4a52799f8909726859b5a9e35c07d751529b83a204da20e64965", slots: []uint32{2}, checkSlots: true, complete: true, text: `pragma no_std;
fn probe(flag: bool, a: &string, b: &string) -> &string {
    let mut alias: &string = a;
    return compare flag {
        _ if { let _ = nothing; alias = b; ret flag; } => alias;
        _ => alias;
    };
}
`},
		{name: "pattern_mismatch", digest: "aae0f88975223a8f72d68be78e8e035cba8983c95ca182dff304ded5e95316a0", slots: []uint32{1, 2}, checkSlots: true, complete: true, text: `pragma no_std;
tag Some<T>(T); type Option<T> = Some(T) | nothing;
fn probe(value: Option<int>, a: &string, b: &string, flag: bool) -> &string {
    let mut alias: &string = a;
    return compare value {
        Some(v) if { let _ = nothing; alias = b; ret flag; } => alias;
        _ => alias;
    };
}
`},
		{name: "pattern_content", digest: "d00748d3fe43be3f3b9d426f25bc924554349360f35378ba43ab3c28f2e2ba9e", slots: []uint32{0, 1}, checkSlots: true, complete: true, text: `pragma no_std;
tag Some<T>(T); type Option<T> = Some(T) | nothing;
fn probe(value: Option<&string>, fallback: &string) -> &string {
    return compare value { Some(v) => v; _ => fallback; };
}
`},
		{name: "binding_content", digest: "0f73d68ecfe59d12ce71b5432034aee46c76e1aadc459e78b4da029dd3098fe0", slots: []uint32{0}, checkSlots: true, complete: true, text: `pragma no_std;
fn probe(value: &string) -> &string {
    return compare value { alias => alias; };
}
`},
		{name: "local_arm_escape", digest: "449e70f885b999d6e16ef76e141fc2f5bb0c2e6a99702f1febc1dee396af88df", owner: "owned", unknown: true, text: `pragma no_std;
fn probe(flag: bool, outside: &string) -> &string {
    return compare flag {
        true => { let owned: string = "owned"; ret &owned; }
        false => outside;
    };
}
`},
		{name: "binding_storage_escape", digest: "bde02c2067e2e9526411991aa64c2cb6effd8bfef0deed35043b0333bf1c818e", owner: "value", unknown: true, text: `pragma no_std;
fn probe(owned: string) -> &string {
    return compare owned { value => &value; };
}
`},
		{name: "user_panic_continues", digest: "2a2688b03749f57bbb1a7df34d1d8dc4e34b08dd680821c5d7311749dc38c042", slots: []uint32{1}, checkSlots: true, complete: true, text: `pragma no_std;
fn panic() -> nothing { return nothing; }
fn probe(flag: bool, value: &string) -> &string {
    compare flag { true => panic(); false => panic(); };
    return value;
}
`},
		{name: "loop_exits", digest: "b00968a86d567e939ed5006b4a97d6c81e27af46a9a9bce12d5f2f56779403b8", slots: []uint32{2}, checkSlots: true, complete: true, text: `pragma no_std;
fn probe(flag: bool, a: &string, b: &string) -> &string {
    let mut alias: &string = a;
    while true {
        compare flag {
            true => { alias = b; break; }
            false => { continue; }
        };
    }
    return alias;
}
`},
		{name: "direct_panic_fallthrough", digest: "150ef551f9ffabbb1777f8889b8e3eb52c8b8169304a69d2c949877e0ffef9fe", unknown: true, text: `pragma no_std;
fn panic() -> nothing { return nothing; }
fn probe() -> &string { panic(); }
`},
		{name: "compare_panic_fallthrough", digest: "4c59c1ea92071c139c7bc968599b7af8b82afcc89b7825ac4057777cbd547af9", unknown: true, text: `pragma no_std;
fn panic() -> nothing { return nothing; }
fn probe(flag: bool) -> &string {
    compare flag { true => panic(); false => panic(); };
}
`},
		{name: "unsupported_guard_is", digest: "ada5b603c73d545690377000bdf51739af4bb939958776944431c01429e59044", text: `pragma no_std;
fn probe(value: int, a: &string, b: &string) -> &string {
    return compare value { _ if value is int => a; _ => b; };
}
`},
		{name: "guard_local_escape", digest: "0d51b085a0153d36d92f7a2436b9c3eefc85d090f5ce4d47039b381bb6888d1d", owner: "owned", slots: []uint32{}, checkSlots: true, complete: true, text: `pragma no_std;
fn read(value: &string) -> int { return 1; }
fn probe(flag: bool) -> int {
    let outside: string = "outside";
    let mut escaped: &string = &outside;
    compare flag {
        _ if { let owned: string = "owned"; escaped = &owned; ret flag; } => nothing;
        _ => nothing;
    };
    return read(escaped);
}
`},
	}
}
