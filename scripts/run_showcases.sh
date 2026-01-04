#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
report_path="${root}/SHOWCASES_REPORT.md"
artifacts_dir="${root}/build/showcases"
strict=1

while [[ $# -gt 0 ]]; do
	case "$1" in
		--report)
			report_path="$2"
			shift 2
			;;
		--artifacts)
			artifacts_dir="$2"
			shift 2
			;;
		--allow-fail)
			strict=0
			shift
			;;
		--strict)
			strict=1
			shift
			;;
		*)
			echo "unknown arg: $1" >&2
			exit 2
			;;
	esac
done

cd "$root"

mkdir -p "$(dirname "$report_path")"
mkdir -p "$artifacts_dir"

surge_bin="${root}/build/surge-showcases"
if [[ ! -x "$surge_bin" ]]; then
	echo ">> building surge for showcases"
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

echo "| Program | VM | LLVM | Notes |" >"$report_path"
echo "| --- | --- | --- | --- |" >>"$report_path"

total=0
failed=0
skipped=0
passed=0

while IFS= read -r sg; do
	[[ -z "$sg" ]] && continue
	rel="${sg#$root/}"
	program="${rel%/main.sg}"
	case_id="$(echo "$rel" | tr '/.' '_')"
	total=$((total + 1))

	args=()
	stdin_payload=""
	case "$rel" in
		"showcases/02_args_echo/main.sg")
			args=("1" "Doe")
			;;
		"showcases/05_collatz/main.sg")
			args=("27" "ignored")
			;;
		"showcases/03_stdin_cat/main.sg")
			stdin_payload="Surge\n"
			;;
	esac

	vm_stdout="$(mktemp)"
	vm_stderr="$(mktemp)"
	vm_exit="$(mktemp)"
	llvm_build_out="$(mktemp)"
	llvm_build_err="$(mktemp)"
	llvm_build_exit="$(mktemp)"
	llvm_stdout="$(mktemp)"
	llvm_stderr="$(mktemp)"
	llvm_exit="$(mktemp)"

	vm_cmd=("$surge_bin" "run" "--backend=vm" "$rel")
	if [[ ${#args[@]} -gt 0 ]]; then
		vm_cmd+=("--" "${args[@]}")
	fi
	SURGE_STDLIB="$root" run_with_stdin "$stdin_payload" "$vm_stdout" "$vm_stderr" "$vm_exit" "${vm_cmd[@]}"
	vm_code="$(cat "$vm_exit")"

	llvm_cmd=("$surge_bin" "build" "--backend=llvm" "--emit-mir" "--emit-llvm" "--keep-tmp" "--print-commands" "$rel")
	SURGE_STDLIB="$root" run_with_stdin "" "$llvm_build_out" "$llvm_build_err" "$llvm_build_exit" "${llvm_cmd[@]}"
	llvm_build_code="$(cat "$llvm_build_exit")"

	llvm_code="1"
	if [[ "$llvm_build_code" -eq 0 ]]; then
		output_name="$(basename "${sg%.sg}")"
		llvm_bin="${root}/build/${output_name}"
		llvm_run_cmd=("$llvm_bin")
		if [[ ${#args[@]} -gt 0 ]]; then
			llvm_run_cmd+=("${args[@]}")
		fi
		run_with_stdin "$stdin_payload" "$llvm_stdout" "$llvm_stderr" "$llvm_exit" "${llvm_run_cmd[@]}"
		llvm_code="$(cat "$llvm_exit")"
	fi

	vm_status="ok"
	llvm_status="ok"
	notes=""

	if [[ "$llvm_build_code" -ne 0 ]]; then
		llvm_status="fail"
		notes="LLVM build failed"
	fi

	if [[ "$vm_code" != "$llvm_code" ]]; then
		vm_status="fail"
		llvm_status="fail"
		notes="exit code mismatch (vm=${vm_code} llvm=${llvm_code})"
	fi

	if [[ "$vm_status" == "ok" && "$llvm_status" == "ok" ]]; then
		if ! cmp -s "$vm_stdout" "$llvm_stdout"; then
			vm_status="fail"
			llvm_status="fail"
			notes="stdout mismatch"
		elif ! cmp -s "$vm_stderr" "$llvm_stderr"; then
			vm_status="fail"
			llvm_status="fail"
			notes="stderr mismatch"
		fi
	fi

	if [[ "$vm_status" == "ok" && "$llvm_status" == "ok" ]]; then
		passed=$((passed + 1))
		echo "| \`${program}\` | ${vm_status} | ${llvm_status} | |" >>"$report_path"
	else
		failed=$((failed + 1))
		case_dir="${artifacts_dir}/${case_id}"
		mkdir -p "$case_dir"
		cp "$vm_stdout" "$case_dir/vm.stdout"
		cp "$vm_stderr" "$case_dir/vm.stderr"
		cp "$vm_exit" "$case_dir/vm.exit_code"
		cp "$llvm_build_out" "$case_dir/llvm_build.stdout"
		cp "$llvm_build_err" "$case_dir/llvm_build.stderr"
		cp "$llvm_build_exit" "$case_dir/llvm_build.exit_code"
		if [[ "$llvm_build_code" -eq 0 ]]; then
			cp "$llvm_stdout" "$case_dir/llvm.stdout"
			cp "$llvm_stderr" "$case_dir/llvm.stderr"
			cp "$llvm_exit" "$case_dir/llvm.exit_code"
			output_name="$(basename "${sg%.sg}")"
			tmp_dir="${root}/build/.tmp/${output_name}"
			if [[ -d "$tmp_dir" ]]; then
				cp -R "$tmp_dir" "$case_dir/llvm_tmp"
			fi
		fi
		artifact_rel="${case_dir#$root/}"
		if [[ -n "$notes" ]]; then
			notes="${notes}; artifacts: ${artifact_rel}"
		else
			notes="artifacts: ${artifact_rel}"
		fi
		echo "| \`${program}\` | ${vm_status} | ${llvm_status} | ${notes} |" >>"$report_path"
	fi

	rm -f "$vm_stdout" "$vm_stderr" "$vm_exit" "$llvm_build_out" "$llvm_build_err" "$llvm_build_exit" "$llvm_stdout" "$llvm_stderr" "$llvm_exit"
done < <(find "$root/showcases" -type f -name "main.sg" | sort)

summary_file="${artifacts_dir}/summary.env"
{
	echo "TOTAL=${total}"
	echo "PASSED=${passed}"
	echo "FAILED=${failed}"
	echo "SKIPPED=${skipped}"
} >"$summary_file"

if [[ "$strict" -eq 1 && "$failed" -gt 0 ]]; then
	exit 1
fi
