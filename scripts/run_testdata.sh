#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backend="vm"
artifacts_dir="${root}/build/testdata"

while [[ $# -gt 0 ]]; do
	case "$1" in
		--backend)
			backend="$2"
			shift 2
			;;
		--artifacts)
			artifacts_dir="$2"
			shift 2
			;;
		*)
			echo "unknown arg: $1" >&2
			exit 2
			;;
	esac
done

cd "$root"

if [[ "$backend" != "vm" && "$backend" != "llvm" ]]; then
	echo "unsupported backend: $backend (expected vm or llvm)" >&2
	exit 2
fi

if [[ "$backend" == "llvm" ]]; then
	if ! command -v clang >/dev/null 2>&1; then
		echo "clang not found; install clang to run LLVM testdata" >&2
		exit 2
	fi
	if ! command -v ar >/dev/null 2>&1; then
		echo "ar not found; install binutils to run LLVM testdata" >&2
		exit 2
	fi
fi

mkdir -p "$artifacts_dir"

surge_bin="${root}/build/surge-testdata"
if [[ ! -x "$surge_bin" ]]; then
	echo ">> building surge for testdata"
	(cd "$root" && go build -o "$surge_bin" ./cmd/surge)
fi

run_with_stdin() {
	local stdin_payload="$1"
	local stdout_file="$2"
	local stderr_file="$3"
	local exit_file="$4"
	shift 4
	local exit_code=0
	if [[ -n "$stdin_payload" ]]; then
		printf "%s" "$stdin_payload" | "$@" >"$stdout_file" 2>"$stderr_file" || exit_code=$?
	else
		"$@" >"$stdout_file" 2>"$stderr_file" || exit_code=$?
	fi
	echo "$exit_code" >"$exit_file"
	return "$exit_code"
}

should_skip_panics() {
	local dir_name="$1"
	local file_name="$2"
	case "$dir_name" in
		vm_numbers|vm_strings|vm_tuples|vm_compare)
			if [[ "$file_name" == *_panics.sg ]]; then
				return 0
			fi
			;;
	esac
	return 1
}

total=0
passed=0
failed=0
skipped=0
failed_cases=()
skipped_cases=()

while IFS= read -r sg; do
	[[ -z "$sg" ]] && continue
	base="${sg%.sg}"
	out_path="${base}.out"
	if [[ ! -f "$out_path" ]]; then
		continue
	fi

	dir_name="$(basename "$(dirname "$sg")")"
	file_name="$(basename "$sg")"

	if should_skip_panics "$dir_name" "$file_name"; then
		skipped=$((skipped + 1))
		skipped_cases+=("${base#$root/}: skipped _panics for ${dir_name}")
		continue
	fi

	flags_path="${base}.flags"
	script_path="${base}.script"
	if [[ "$backend" == "llvm" && ( -f "$flags_path" || -f "$script_path" ) ]]; then
		skipped=$((skipped + 1))
		reason="unsupported flags"
		if [[ -f "$script_path" ]]; then
			reason="vm-debug script only"
		fi
		skipped_cases+=("${base#$root/}: ${reason}")
		continue
	fi

	total=$((total + 1))

	expected_code=0
	if [[ -f "${base}.code" ]]; then
		expected_code="$(tr -d ' \n' < "${base}.code")"
		if [[ -z "$expected_code" ]]; then
			expected_code=0
		fi
	fi

	stdin_payload=""
	if [[ -f "${base}.stdin" ]]; then
		stdin_payload="$(cat "${base}.stdin")"
	fi

	args=()
	if [[ -f "${base}.args" ]]; then
		while IFS= read -r line; do
			line="$(echo "$line" | sed 's/[[:space:]]*$//')"
			if [[ -n "$line" ]]; then
				args+=("$line")
			fi
		done < "${base}.args"
	fi

	actual_stdout="$(mktemp)"
	actual_stderr="$(mktemp)"
	actual_exit="$(mktemp)"
	build_stdout="$(mktemp)"
	build_stderr="$(mktemp)"
	build_exit="$(mktemp)"

	run_cmd=()
	build_cmd=()
	build_code=0
	run_code=0

	if [[ "$backend" == "vm" ]]; then
		run_cmd=("$surge_bin" "run" "--backend=vm")
		if [[ -f "$script_path" ]]; then
			run_cmd+=("--vm-debug" "--vm-debug-script" "$script_path")
		fi
		if [[ -f "$flags_path" ]]; then
			while IFS= read -r line; do
				line="$(echo "$line" | sed 's/[[:space:]]*$//')"
				if [[ -n "$line" ]]; then
					run_cmd+=("$line")
				fi
			done < "$flags_path"
		fi
		run_cmd+=("${sg#$root/}")
		if [[ ${#args[@]} -gt 0 ]]; then
			run_cmd+=("--" "${args[@]}")
		fi
		SURGE_STDLIB="$root" run_with_stdin "$stdin_payload" "$actual_stdout" "$actual_stderr" "$actual_exit" "${run_cmd[@]}"
		run_code="$(cat "$actual_exit")"
	else
		build_cmd=("$surge_bin" "build" "--backend=llvm" "--emit-mir" "--emit-llvm" "--keep-tmp" "--print-commands" "${sg#$root/}")
		SURGE_STDLIB="$root" run_with_stdin "" "$build_stdout" "$build_stderr" "$build_exit" "${build_cmd[@]}"
		build_code="$(cat "$build_exit")"
		if [[ "$build_code" -eq 0 ]]; then
			output_name="$(basename "${sg%.sg}")"
			llvm_bin="${root}/build/${output_name}"
			run_cmd=("$llvm_bin")
			if [[ ${#args[@]} -gt 0 ]]; then
				run_cmd+=("${args[@]}")
			fi
			run_with_stdin "$stdin_payload" "$actual_stdout" "$actual_stderr" "$actual_exit" "${run_cmd[@]}"
			run_code="$(cat "$actual_exit")"
		else
			run_code=1
		fi
	fi

	fail_reason=""
	if [[ "$backend" == "llvm" && "$build_code" -ne 0 ]]; then
		fail_reason="LLVM build failed"
	elif [[ "$run_code" != "$expected_code" ]]; then
		fail_reason="exit code mismatch (expected ${expected_code} got ${run_code})"
	else
		if [[ "$expected_code" -eq 0 ]]; then
			if [[ -s "$actual_stderr" ]]; then
				fail_reason="unexpected stderr for success case"
			elif ! cmp -s "$out_path" "$actual_stdout"; then
				fail_reason="stdout mismatch"
			fi
		else
			if [[ -s "$actual_stdout" ]]; then
				fail_reason="unexpected stdout for error case"
			elif ! cmp -s "$out_path" "$actual_stderr"; then
				fail_reason="stderr mismatch"
			fi
		fi
	fi

	if [[ -z "$fail_reason" ]]; then
		passed=$((passed + 1))
	else
		failed=$((failed + 1))
		case_id="$(echo "${base#$root/}" | tr '/.' '_')"
		case_dir="${artifacts_dir}/${backend}/${case_id}"
		mkdir -p "$case_dir"
		printf "%s\n" "$fail_reason" >"$case_dir/failure.txt"
		printf "%s\n" "${run_cmd[*]}" >"$case_dir/run_cmd.txt"
		if [[ "$backend" == "llvm" ]]; then
			printf "%s\n" "${build_cmd[*]}" >"$case_dir/build_cmd.txt"
			cp "$build_stdout" "$case_dir/build.stdout"
			cp "$build_stderr" "$case_dir/build.stderr"
			cp "$build_exit" "$case_dir/build.exit_code"
		fi
		cp "$actual_stdout" "$case_dir/run.stdout"
		cp "$actual_stderr" "$case_dir/run.stderr"
		cp "$actual_exit" "$case_dir/run.exit_code"
		cp "$out_path" "$case_dir/expected.out"
		printf "%s\n" "$expected_code" >"$case_dir/expected.code"
		if [[ -n "$stdin_payload" ]]; then
			printf "%s" "$stdin_payload" >"$case_dir/stdin.txt"
		fi
		if [[ ${#args[@]} -gt 0 ]]; then
			printf "%s\n" "${args[@]}" >"$case_dir/args.txt"
		fi
		if [[ "$expected_code" -eq 0 ]]; then
			diff -u "$out_path" "$actual_stdout" >"$case_dir/output.diff" || true
		else
			diff -u "$out_path" "$actual_stderr" >"$case_dir/output.diff" || true
		fi
		if [[ "$backend" == "llvm" && "$build_code" -eq 0 ]]; then
			output_name="$(basename "${sg%.sg}")"
			tmp_dir="${root}/build/.tmp/${output_name}"
			if [[ -d "$tmp_dir" ]]; then
				cp -R "$tmp_dir" "$case_dir/llvm_tmp"
			fi
		fi
		failed_cases+=("${base#$root/}: ${fail_reason}")
	fi

	rm -f "$actual_stdout" "$actual_stderr" "$actual_exit" "$build_stdout" "$build_stderr" "$build_exit"
done < <(find "$root/testdata/golden" -type f -name "*.sg" | sort)

summary_dir="${artifacts_dir}/${backend}"
mkdir -p "$summary_dir"
summary_file="${summary_dir}/summary.env"
{
	echo "TOTAL=${total}"
	echo "PASSED=${passed}"
	echo "FAILED=${failed}"
	echo "SKIPPED=${skipped}"
} >"$summary_file"

if [[ ${#failed_cases[@]} -gt 0 ]]; then
	printf "%s\n" "${failed_cases[@]}" >"${summary_dir}/failed_cases.txt"
fi

if [[ ${#skipped_cases[@]} -gt 0 ]]; then
	printf "%s\n" "${skipped_cases[@]}" >"${summary_dir}/skipped_cases.txt"
fi

if [[ "$failed" -gt 0 ]]; then
	exit 1
fi
