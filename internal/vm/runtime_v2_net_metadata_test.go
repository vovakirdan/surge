//go:build runtime_v2_pending

package vm_test

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRuntimeV2NetMetadataStaticShape(t *testing.T) {
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("clang is required for Runtime V2 pending static check: %v", err)
	}

	source := `
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "rt.h"
#include "rt_net_handles.h"

void* (*runtime_v2_check_net_listen)(void*, uint64_t) = rt_net_listen;
void* (*runtime_v2_check_net_connect)(void*, uint64_t) = rt_net_connect;
void* (*runtime_v2_check_net_close_listener)(void*) = rt_net_close_listener;
void* (*runtime_v2_check_net_close_conn)(void*) = rt_net_close_conn;
void* (*runtime_v2_check_net_accept)(const void*) = rt_net_accept;
void* (*runtime_v2_check_net_read)(const void*, uint8_t*, uint64_t) = rt_net_read;
void* (*runtime_v2_check_net_write)(const void*, const uint8_t*, uint64_t) = rt_net_write;
void* (*runtime_v2_check_net_read_bytes)(const void*, uint64_t) = rt_net_read_bytes;
void* (*runtime_v2_check_net_write_bytes)(const void*, const void*, uint64_t, uint64_t) = rt_net_write_bytes;
bool (*runtime_v2_check_net_wait_accept)(const void*) = rt_net_wait_accept;
bool (*runtime_v2_check_net_wait_readable)(const void*) = rt_net_wait_readable;
bool (*runtime_v2_check_net_wait_writable)(const void*) = rt_net_wait_writable;

_Static_assert(NET_LISTENER_SINGLE == 0, "single listener discriminator must stay stable");
_Static_assert(NET_LISTENER_REUSEPORT_GROUP != NET_LISTENER_SINGLE,
               "reuseport group discriminator must be distinct");
_Static_assert(NET_LISTENER_FALLBACK_HANDOFF != NET_LISTENER_SINGLE,
               "fallback handoff discriminator must be distinct");

_Static_assert(sizeof(((NetListenerMember*)0)->fd) == sizeof(int),
               "listener member must carry fd");
_Static_assert(sizeof(((NetListenerMember*)0)->owner_shard_id) == sizeof(uint32_t),
               "listener member must carry owner shard id");
_Static_assert(sizeof(((NetListenerMember*)0)->closed) == sizeof(bool),
               "listener member must carry closed state");
_Static_assert(offsetof(NetListener, fd) == 0,
               "listener must preserve legacy fd prefix for copied handles");
_Static_assert(sizeof(((NetListener*)0)->fd) == sizeof(int),
               "listener must carry compatibility fd");
_Static_assert(sizeof(((NetListener*)0)->kind) == sizeof(uint8_t),
               "listener must carry discriminator");
_Static_assert(sizeof(((NetListener*)0)->closed) == sizeof(bool),
               "listener must carry logical closed state");
_Static_assert(sizeof(((NetListener*)0)->owner_shard_id) == sizeof(uint32_t),
               "listener must carry compatibility owner shard");
_Static_assert(sizeof(((NetListener*)0)->member_count) == sizeof(size_t),
               "listener must carry member count");
_Static_assert(sizeof(((NetListener*)0)->members) == sizeof(NetListenerMember*),
               "listener must own member array");
_Static_assert(sizeof(((NetConn*)0)->fd) == sizeof(int), "connection must carry fd");
_Static_assert(sizeof(((NetConn*)0)->closed) == sizeof(bool),
               "connection must carry closed state");
_Static_assert(sizeof(((NetConn*)0)->owner_shard_valid) == sizeof(uint8_t),
               "connection must carry owner validity");
_Static_assert(sizeof(((NetConn*)0)->owner_shard_id) == sizeof(uint32_t),
               "connection must carry owner shard id");
`

	cmd := exec.Command(
		clang,
		"-std=c11",
		"-Wall",
		"-Wextra",
		"-Werror",
		"-fsyntax-only",
		"-I"+filepath.Join(root, "runtime", "native"),
		"-x",
		"c",
		"-",
	)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Runtime V2 net metadata static shape check failed:\n%s", output)
	}
}

const runtimeV2NetListenCloseSource = `import stdlib/net as net;

@entrypoint("argv")
fn main(port: uint) -> int {
    let listen_res = net.listen("127.0.0.1", port);
    compare listen_res {
        Success(listener) => {
            let close_res = net.close_listener(own listener);
            return compare close_res {
                Success(_) => 0;
                _ => 2;
            };
        }
        err => return err.code to int;
    };
    return 3;
}
`

func TestRuntimeV2NetMetadataMultiShardListenClose(t *testing.T) {
	ensureLLVMToolchain(t)
	port := pickFreePort(t)
	env := runtimeV2AcceptEnv(t)
	env = overrideEnvVar(env, "SURGE_SHARDS", "4")
	env = overrideEnvVar(env, "SURGE_THREADS", "4")
	srv := startRuntimeV2AcceptServer(t, runtimeV2NetListenCloseSource, env, strconv.Itoa(port))
	srv.waitExitZero(15 * time.Second)
}
