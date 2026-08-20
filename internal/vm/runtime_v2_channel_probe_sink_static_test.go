package vm_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A non-blocking channel receive has nowhere to put the value unless the
// caller gives it one. The core (rt_channel_try_recv_status_owner_locked in
// runtime/native/rt_channel_sync.c) is the only place that can pop a buffered
// entry or ack a parked sender, and both are irreversible: a caller passing no
// sink is not asking whether an arm is ready, it is destroying the answer
// where nobody can see it. The core answers that with a panic
// ("async: channel try-recv with nowhere to put the value"), which is a
// runtime failure — this guard is the compile-time twin, so the shape is
// caught by the pre-commit hook instead of by whoever next runs the transport
// gate.
//
// It has already been needed once. The commit that outlawed the null sink
// converted the one runtime caller (select's readiness probe, which now takes
// the value into a real sink and hands it to rt_channel_release_payload once
// the control lock is off) and missed the two probe helpers in
// internal/vm/runtime_v2_remote_publication_test.go, which had been legal when
// they were written. Those two null sinks then panicked seven of the eight
// rows of TestRuntimeV2RemoteSelectAbandonEdges and reddened the transport
// gate, invisibly to `make check` because that harness sits behind a build
// tag. This test carries no build tag on purpose.
//
// Scope, stated honestly: this catches a LITERAL NULL in the sink position.
// A variable that happens to be null at run time is the core's panic to
// report, not this test's.
func TestRuntimeV2ChannelRecvSinkIsNeverNull(t *testing.T) {
	root := repoRoot(t)

	// Every non-blocking receive entry point that reaches the core's sink
	// check, plus the blocking helper that forwards its caller's pointer
	// straight into it on the non-task lane.
	entryPoints := []string{
		"rt_channel_try_recv_status_owner_locked",
		"rt_channel_try_recv_status_locked",
		"rt_channel_try_recv",
		"rt_channel_recv_blocking",
		"rt_channel_recv",
	}

	var offences []string
	// Build outputs carry COPIES of the runtime sources, frozen at whatever
	// they said when that artefact was produced. Scanning them makes this guard
	// report a defect that was fixed months ago and cannot be fixed again — it
	// fired on eight stale `build/showcases/**/native_runtime/rt_async_select.c`
	// the first time it ran outside a fresh worktree.
	skipDirs := map[string]bool{
		".git": true, "vendor": true, "node_modules": true,
		"build": true, "target": true, ".tmp": true,
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".c", ".h", ".go":
		default:
			return nil
		}
		// The C harnesses live inside Go string constants, so .go files are
		// scanned as text exactly like the runtime sources are.
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if d.Name() == thisGuardFile {
			return nil // the guard names the shape it forbids
		}
		source := string(data)
		for _, name := range entryPoints {
			for _, args := range callArguments(source, name) {
				if lastArgument(args) == "NULL" {
					offences = append(offences,
						rel+": "+name+"("+args+")")
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	if len(offences) > 0 {
		t.Fatalf("a channel receive was handed a null sink, which destroys the value it "+
			"claims to be asking about; take the value into a real sink and release it "+
			"through rt_channel_release_payload instead:\n  %s",
			strings.Join(offences, "\n  "))
	}
}

const thisGuardFile = "runtime_v2_channel_probe_sink_static_test.go"

// callArguments returns the raw argument text of every call to name in source.
// A call is a use of the identifier followed by "(", so a declaration
// ("uint8_t rt_channel_try_recv(void* channel, uint64_t* out_bits);") is
// matched too — harmless, because a declaration's last argument is a
// parameter, never the literal NULL.
func callArguments(source, name string) []string {
	var out []string
	for offset := 0; ; {
		idx := strings.Index(source[offset:], name+"(")
		if idx < 0 {
			return out
		}
		start := offset + idx
		offset = start + len(name) + 1
		// Reject a longer identifier that merely ends with this name, so
		// rt_channel_try_recv does not swallow rt_channel_try_recv_status_locked.
		if start > 0 && isIdentifierByte(source[start-1]) {
			continue
		}
		depth := 1
		i := offset
		for ; i < len(source) && depth > 0; i++ {
			switch source[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
		}
		if depth != 0 {
			return out
		}
		out = append(out, source[offset:i-1])
	}
}

func lastArgument(args string) string {
	depth := 0
	cut := 0
	for i := range len(args) {
		switch args[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				cut = i + 1
			}
		}
	}
	return strings.TrimSpace(args[cut:])
}

func isIdentifierByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}
