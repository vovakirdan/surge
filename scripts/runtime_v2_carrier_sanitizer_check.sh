#!/usr/bin/env bash
# Runtime V2 carrier sanitizer gate — the executable half of
# `make runtime-v2-carrier-sanitizer-check`.
#
# Epic 23b section 12 states the contract this file implements: the gate
# "first requires Valgrind, ASan/UBSan, and TSan availability, then runs the
# focused carrier rows with skip-on-missing disabled; any skip or unavailable
# tool fails the target". Two modes, both driven from the Makefile:
#
#   preflight          Prove Valgrind, ASan/UBSan and TSan are present AND
#                      working on this host, before a single carrier row runs.
#                      A missing or non-working tool fails here, loudly, by
#                      name. Nothing in this file may skip.
#   run [--expect A,B --] <command...>
#                      Run one carrier row and fail the gate unless the row
#                      really ran: a skip, an empty selection, a nonzero exit,
#                      or a row that did not execute every test named by
#                      --expect all fail.
#
# Why a row must NAME the tests it executes: a `-run` alternation that has lost
# a member is still a valid selection. `go test` prints ok and exits 0 for the
# survivors, so a row whose two sanitizer tests were deleted or renamed away
# stays green on one 0.00s unit test with zero sanitizer execution — the exact
# absent-gate shape this target exists to remove. Exit status cannot see it;
# only an explicit expected-name list can. Hence --expect is mandatory for
# every `go test` row, and each name must be observed PASSING: a `--- PASS:`
# line proves the test finished under the tool, where a start line would also
# be printed by a test the sanitizer killed halfway.
#
# Why the preflight plants defects instead of calling `command -v`: presence is
# not availability. A host can carry clang and still be unable to START a TSan
# binary, and a build whose instrumentation silently did nothing would sail
# through every clean row while proving nothing at all. So each tool is asked
# to CATCH a planted defect and to leave a clean probe alone.
#
# Every planted defect below is deterministic — a leak, a use-after-free, a
# signed overflow, an unlock of an unlocked mutex. Deliberately not a data
# race: a probe that is right on 99 runs out of 100 turns a mandatory gate into
# a coin flip, and a gate that goes green on a re-run is the habit this epic
# exists to stop.

set -euo pipefail

readonly GATE="runtime-v2-carrier-sanitizer-check"

# Exit codes chosen so a probe that "fails" for an unrelated reason (a crash,
# a signal, a wrong binary) cannot be mistaken for the planted defect firing.
readonly VALGRIND_ERROR_EXIT=97
readonly TSAN_ERROR_EXIT=66

# Every probe is bounded. A sanitizer report normally takes milliseconds, but
# the reporting path can wedge: llvm-symbolizer deadlocked here when a TSan
# report was written into a capturing pipe, which hung the whole gate with no
# output. Hence symbolize=0 on the reporting probes below (the report line is
# what is matched, never the stack) and a hard bound on every probe, so a
# wedged tool fails this gate instead of stalling it.
readonly PROBE_TIMEOUT=120

die() {
    printf '%s: %s\n' "$GATE" "$*" >&2
    exit 1
}

note() {
    printf '%s: %s\n' "$GATE" "$*"
}

probe_source() {
    cat <<'PROBE_C'
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int shared;
/* The sink makes an allocation escape so the optimizer cannot delete the
   planted leak; clearing it again leaves the block unreachable at exit, which
   is what makes valgrind call it "definitely lost" rather than "reachable". */
static int* volatile sink;

static void* bump(void* unused) {
    (void)unused;
    shared += 1;
    return NULL;
}

int main(int argc, char** argv) {
    const char* mode = argc > 1 ? argv[1] : "clean";
    pthread_t worker;
    if (pthread_create(&worker, NULL, bump, NULL) != 0) {
        return 70;
    }
    if (pthread_join(worker, NULL) != 0) {
        return 71;
    }
    if (strcmp(mode, "leak") == 0) {
        int* leaked = (int*)malloc(sizeof(int) * 8);
        if (leaked == NULL) {
            return 72;
        }
        sink = leaked;
        leaked[0] = shared;
        printf("leak %d\n", leaked[0]);
        sink = NULL;
        return 0;
    }
    if (strcmp(mode, "use-after-free") == 0) {
        int* cell = (int*)malloc(sizeof(int));
        if (cell == NULL) {
            return 73;
        }
        free(cell);
        *cell = shared;
        printf("use-after-free %d\n", *cell);
        return 0;
    }
    if (strcmp(mode, "overflow") == 0) {
        int wide = 2147483647;
        wide += shared;
        printf("overflow %d\n", wide);
        return 0;
    }
    if (strcmp(mode, "bad-unlock") == 0) {
        pthread_mutex_t mutex;
        if (pthread_mutex_init(&mutex, NULL) != 0) {
            return 74;
        }
        pthread_mutex_unlock(&mutex);
        pthread_mutex_destroy(&mutex);
        printf("bad-unlock %d\n", shared);
        return 0;
    }
    printf("clean %d\n", shared);
    return 0;
}
PROBE_C
}

# probe_expect <label> <clean|detects> <expected-substring> <command...>
#   clean   — the command must exit 0 and the substring must be absent.
#   detects — the command must exit nonzero and print the substring, i.e. the
#             tool caught the planted defect.
probe_expect() {
    local label="$1" expectation="$2" needle="$3"
    shift 3
    local output status=0
    set +e
    output="$(timeout "$PROBE_TIMEOUT" "$@" 2>&1)"
    status=$?
    set -e
    if [ "$status" -eq 124 ]; then
        die "$label: the probe did not finish within ${PROBE_TIMEOUT}s.
$output"
    fi
    case "$expectation" in
        clean)
            if [ "$status" -ne 0 ]; then
                die "$label: the clean probe exited $status; this host cannot run the tool.
$output"
            fi
            if [ -n "$needle" ] && [[ $output == *"$needle"* ]]; then
                die "$label: the clean probe reported a defect it should not see.
$output"
            fi
            ;;
        detects)
            if [ "$status" -eq 0 ]; then
                die "$label: the planted defect was NOT caught (probe exited 0).
The tool is installed but is not instrumenting; a green carrier row under it
would prove nothing.
$output"
            fi
            if [[ $output != *"$needle"* ]]; then
                die "$label: the planted defect did not produce the expected report \"$needle\".
$output"
            fi
            ;;
        *)
            die "internal: unknown expectation $expectation"
            ;;
    esac
    note "OK   $label"
}

build_probe() {
    local label="$1" binary="$2"
    shift 2
    local output status=0
    set +e
    output="$(clang -std=c11 -O1 -g -pthread -fno-omit-frame-pointer \
        "$@" "$WORKDIR/probe.c" -o "$binary" 2>&1)"
    status=$?
    set -e
    if [ "$status" -ne 0 ]; then
        die "$label is unavailable: clang could not build the probe with $*.
$output"
    fi
}

preflight() {
    # The reference runner is named by the epic. The carrier rows below are
    # calibrated on it, so a different host is refused rather than measured.
    case "$MACHTYPE" in
        x86_64-*-linux-gnu*) ;;
        *) die "the reference runner is x86_64-linux-gnu; this host reports MACHTYPE=$MACHTYPE" ;;
    esac

    command -v valgrind >/dev/null 2>&1 ||
        die "Valgrind is not on PATH. It is a mandatory tool for this gate and
the gate may not be run without it (install: sudo apt-get install -y valgrind)."
    command -v clang >/dev/null 2>&1 ||
        die "clang is not on PATH; ASan/UBSan and TSan are mandatory tools for
this gate (install: sudo apt-get install -y clang)."
    command -v timeout >/dev/null 2>&1 ||
        die "timeout is not on PATH; every sanitizer probe must be bounded."

    WORKDIR="$(mktemp -d)"
    trap 'rm -rf "$WORKDIR"' EXIT
    probe_source >"$WORKDIR/probe.c"

    build_probe "Valgrind" "$WORKDIR/probe_plain"
    build_probe "ASan/UBSan" "$WORKDIR/probe_asan" \
        -fsanitize=address,undefined -fno-sanitize-recover=all
    build_probe "TSan" "$WORKDIR/probe_tsan" -fsanitize=thread

    local -a valgrind_cmd=(
        valgrind
        "--error-exitcode=$VALGRIND_ERROR_EXIT"
        --leak-check=full
        --errors-for-leak-kinds=definite
        -q
    )
    probe_expect "Valgrind runs a clean program" clean "definitely lost" \
        "${valgrind_cmd[@]}" "$WORKDIR/probe_plain" clean
    probe_expect "Valgrind catches a definite leak" detects "definitely lost" \
        "${valgrind_cmd[@]}" "$WORKDIR/probe_plain" leak

    probe_expect "ASan/UBSan runs a clean program" clean "Sanitizer:" \
        env ASAN_OPTIONS=detect_leaks=1:symbolize=0 "$WORKDIR/probe_asan" clean
    probe_expect "ASan catches a use-after-free" detects \
        "AddressSanitizer: heap-use-after-free" \
        env ASAN_OPTIONS=detect_leaks=1:symbolize=0 \
        "$WORKDIR/probe_asan" use-after-free
    probe_expect "UBSan catches a signed overflow" detects \
        "runtime error: signed integer overflow" \
        env UBSAN_OPTIONS=symbolize=0 "$WORKDIR/probe_asan" overflow

    probe_expect "TSan runs a clean program" clean "WARNING: ThreadSanitizer" \
        env TSAN_OPTIONS=halt_on_error=1:symbolize=0 "$WORKDIR/probe_tsan" clean
    probe_expect "TSan catches an unlock of an unlocked mutex" detects \
        "WARNING: ThreadSanitizer: unlock of an unlocked mutex" \
        env "TSAN_OPTIONS=halt_on_error=1:symbolize=0:exitcode=$TSAN_ERROR_EXIT" \
        "$WORKDIR/probe_tsan" bad-unlock

    note "all three sanitizer families are available and instrumenting"
}

looks_like_go_test() {
    local executable="$1"
    shift
    [ "${executable##*/}" = "go" ] || return 1
    local argument
    for argument in "$@"; do
        [ "$argument" = "test" ] && return 0
    done
    return 1
}

has_argument() {
    local wanted="$1"
    shift
    local argument
    for argument in "$@"; do
        [ "$argument" = "$wanted" ] && return 0
    done
    return 1
}

run_row() {
    # Leading options belong to the wrapper; everything from the first
    # non-option (or from `--`) is the row's own command.
    local -a expected=()
    local name
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --expect)
                shift
                [ "$#" -gt 0 ] ||
                    die "--expect needs a comma-separated list of test names"
                IFS=',' read -r -a expected <<<"$1"
                for name in ${expected[@]+"${expected[@]}"}; do
                    case "$name" in
                        "" | *[!A-Za-z0-9_/]*)
                            die "--expect takes plain test names, got \"$name\"" ;;
                    esac
                done
                [ "${#expected[@]}" -ne 0 ] || die "--expect list is empty"
                shift
                ;;
            --)
                shift
                break
                ;;
            *)
                break
                ;;
        esac
    done

    [ "$#" -gt 0 ] || die "run needs a command"

    if looks_like_go_test "$@"; then
        # Without -v a skipped test prints nothing at all and the package
        # still reports ok: the row would be silently absent from a green gate.
        has_argument -v "$@" ||
            die "row must pass -v so a skip is visible: $*"
        # And without the expected names, a row that lost part of its
        # selection is indistinguishable from one that ran in full.
        [ "${#expected[@]}" -ne 0 ] ||
            die "a go test row must declare the tests it executes with --expect: $*"
    fi

    local log status=0
    log="$(mktemp)"
    note "row: $*"
    set +e
    "$@" 2>&1 | tee "$log"
    status="${PIPESTATUS[0]}"
    set -e

    local -a offences=()
    local line
    while IFS= read -r line; do
        case "$line" in
            *"--- SKIP"*|*"[no tests to run]"*|*"no tests to run"*|*"no test files"*)
                offences+=("$line")
                ;;
        esac
    done <"$log"

    local -a unseen=()
    for name in ${expected[@]+"${expected[@]}"}; do
        # Anchored and terminated so a subtest or a longer test name whose
        # prefix matches cannot stand in for the test that is required.
        grep -Eq "^[[:space:]]*--- PASS: ${name}([[:space:]]|\$)" "$log" ||
            unseen+=("$name")
    done
    rm -f "$log"

    if [ "${#offences[@]}" -ne 0 ]; then
        printf '%s: row did not run with skip-on-missing disabled:\n' "$GATE" >&2
        printf '  %s\n' "${offences[@]}" >&2
        die "a skipped or unselected carrier row fails this gate (section 12)"
    fi
    if [ "$status" -ne 0 ]; then
        die "row failed with exit $status: $*"
    fi
    if [ "${#unseen[@]}" -ne 0 ]; then
        printf '%s: row exited 0 without executing every test it must:\n' "$GATE" >&2
        printf '  %s never printed "--- PASS:"\n' "${unseen[@]}" >&2
        die "a partially selected carrier row fails this gate: the missing tests
were deleted, renamed, or filtered out of the row's -run selection, and the
survivors passing proves nothing about them"
    fi
}

main() {
    local mode="${1-}"
    case "$mode" in
        preflight)
            shift
            [ "$#" -eq 0 ] || die "preflight takes no arguments"
            preflight
            ;;
        run)
            shift
            run_row "$@"
            ;;
        *)
            die "usage: $0 preflight | $0 run <command...>"
            ;;
    esac
}

main "$@"
