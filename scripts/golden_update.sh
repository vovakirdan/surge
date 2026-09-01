#!/usr/bin/env bash
set -euo pipefail
umask 022
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"
LIVE_GOLDEN_DIR="${ROOT_DIR}/testdata/golden"
SURGE_BIN="${SURGE_BIN:-${ROOT_DIR}/surge}"
DIAG_EXPECTATIONS="${SURGE_GOLDEN_DIAG_ZERO_EXPECTATIONS:-}"
FMT_EXPECTATIONS="${SURGE_GOLDEN_FMT_FAILURE_EXPECTATIONS:-}"
EMIT_EXPECTATIONS="${SURGE_GOLDEN_EMIT_FAILURE_EXPECTATIONS:-}"
if [[ ! -r "${DIAG_EXPECTATIONS}" || ! -r "${FMT_EXPECTATIONS}" || ! -r "${EMIT_EXPECTATIONS}" ]]; then
	echo "golden expectation transport is missing; run 'make golden-update'" >&2
	exit 1
fi
if [[ ! -x "${SURGE_BIN}" ]]; then
	if command -v surge >/dev/null 2>&1; then
		SURGE_BIN="$(command -v surge)"
	else
		echo "surge binary not found. Build ./surge or set SURGE_BIN." >&2
		exit 1
	fi
fi
validate_corpus_tree() {
	local scan_root="$1" unsafe
	[[ ! -L "${scan_root}" && -d "${scan_root}" ]] || { echo "golden corpus root is not a real directory: ${scan_root}" >&2; exit 1; }
	unsafe="$(find -P "${scan_root}" ! -type f ! -type d -print -quit)" || { echo "failed to validate golden corpus: ${scan_root}" >&2; exit 1; }
	[[ -z "${unsafe}" ]] || { printf 'golden corpus contains unsafe filesystem entry: %s\n' "${unsafe}" >&2; exit 1; }
}
validate_corpus_tree "${LIVE_GOLDEN_DIR}"
export SURGE_STDLIB="${ROOT_DIR}"
declare -A EXPECT_DIAG_ZERO=()
declare -A EXPECT_FMT_FAILURE=()
declare -A EXPECT_EMIT_FAILURE=()
declare -A SEEN_DIAG_ZERO=()
declare -A SEEN_FMT_FAILURE=()
declare -A SEEN_EMIT_FAILURE=()
declare -a DIAG_ZERO_PATHS=()
declare -a FMT_FAILURE_PATHS=()
declare -a EMIT_FAILURE_KEYS=()
load_expectations() {
	local filename="$1"
	local -n expectation_map="$2"
	local -n expectation_paths="$3"
	local golden_path
	while IFS= read -r -d '' golden_path; do
		if [[ -z "${golden_path}" || -n "${expectation_map[${golden_path}]+present}" ]]; then
			echo "invalid or duplicate expectation path" >&2
			exit 1
		fi
		expectation_map["${golden_path}"]=1
		expectation_paths+=("${golden_path}")
	done < "${filename}"
}
load_expectations "${DIAG_EXPECTATIONS}" EXPECT_DIAG_ZERO DIAG_ZERO_PATHS
load_expectations "${FMT_EXPECTATIONS}" EXPECT_FMT_FAILURE FMT_FAILURE_PATHS

while IFS= read -r -d '' phase && IFS= read -r -d '' golden_path; do
	emit_key="${phase}:${golden_path}"
	if [[ -z "${phase}" || -z "${golden_path}" || -n "${EXPECT_EMIT_FAILURE[${emit_key}]+present}" ]]; then
		echo "invalid or duplicate emit expectation" >&2
		exit 1
	fi
	EXPECT_EMIT_FAILURE["${emit_key}"]=1
	EMIT_FAILURE_KEYS+=("${emit_key}")
done < "${EMIT_EXPECTATIONS}"
TEMP_DIR=""
STAGED_ROOT=""
STAGED_GOLDEN_DIR=""
BACKUP_GOLDEN_DIR=""
FAILED_GOLDEN_DIR=""
ERRORS_FILE=""
SOURCES_FILE=""
cleanup() {
	local status=$?
	local remove_temp=1
	trap - EXIT INT TERM HUP
	if ! cd "${ROOT_DIR}"; then
		status=1
	fi
	if [[ "${status}" -ne 0 && -e "${LIVE_GOLDEN_DIR}" && -e "${BACKUP_GOLDEN_DIR}" ]]; then
		if ! mv "${LIVE_GOLDEN_DIR}" "${FAILED_GOLDEN_DIR}"; then
			echo "failed to set aside generated golden corpus during rollback" >&2
			status=1
			remove_temp=0
		elif ! mv "${BACKUP_GOLDEN_DIR}" "${LIVE_GOLDEN_DIR}"; then
			echo "failed to restore live golden corpus during rollback" >&2
			status=1
			remove_temp=0
		fi
	fi
	if [[ ! -e "${LIVE_GOLDEN_DIR}" && -e "${BACKUP_GOLDEN_DIR}" ]]; then
		if ! mv "${BACKUP_GOLDEN_DIR}" "${LIVE_GOLDEN_DIR}"; then
			echo "failed to restore live golden corpus" >&2
			status=1
			remove_temp=0
		fi
	fi
	if [[ "${remove_temp}" -eq 1 && -n "${TEMP_DIR}" ]]; then
		if ! rm -rf "${TEMP_DIR}"; then
			status=1
		fi
	elif [[ "${remove_temp}" -ne 1 ]]; then
		echo "golden recovery data preserved at ${TEMP_DIR}" >&2
	fi
	exit "${status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP
TEMP_DIR="$(mktemp -d "${ROOT_DIR}/testdata/.golden-update.XXXXXX")"
STAGED_ROOT="${TEMP_DIR}/worktree"
STAGED_GOLDEN_DIR="${STAGED_ROOT}/testdata/golden"
BACKUP_GOLDEN_DIR="${TEMP_DIR}/original"
FAILED_GOLDEN_DIR="${TEMP_DIR}/failed"
ERRORS_FILE="${TEMP_DIR}/errors"
SOURCES_FILE="${TEMP_DIR}/sources"
touch "${ERRORS_FILE}" "${SOURCES_FILE}"
if ! mkdir -p "${STAGED_ROOT}/testdata" || ! cp -a "${LIVE_GOLDEN_DIR}" "${STAGED_GOLDEN_DIR}"; then
	echo "failed to stage live golden corpus" >&2
	exit 1
fi
GOLDEN_DIR="${STAGED_GOLDEN_DIR}"
CORE_GOLDEN_DIR="${GOLDEN_DIR}/core_stdlib"
cd "${STAGED_ROOT}"
record_error() {
	printf '%s\n' "$1" >> "${ERRORS_FILE}"
}

run_required() {
	local phase="$1"
	local output="$2"
	shift 2
	local status
	if "$@" > "${output}" 2>/dev/null; then
		status=0
	else
		status=$?
	fi
	if [[ "${status}" -ne 0 ]]; then
		record_error "${phase} failed with status ${status}: $*"
	fi
}

run_emit() {
	local phase="$1"
	local label="$2"
	local rel="$3"
	local output="$4"
	shift 4
	local status emit_key
	if "$@" > "${output}" 2>&1; then
		status=0
	else
		status=$?
	fi
	emit_key="${phase}:${rel}"
	if [[ -n "${EXPECT_EMIT_FAILURE[${emit_key}]+present}" ]]; then
		SEEN_EMIT_FAILURE["${emit_key}"]=1
		if [[ "${status}" -eq 0 ]]; then
			record_error "expected ${label} failure became successful: ${rel}"
		elif [[ "${status}" -ne 1 ]]; then
			record_error "${label} failure status ${status}, want 1: ${rel}"
		elif [[ ! -s "${output}" ]]; then
			record_error "expected ${label} failure produced no output: ${rel}"
		fi
	elif [[ "${status}" -ne 0 ]]; then
		record_error "unlisted ${label} failure with status ${status}: ${rel}"
	fi
}

generate_outputs() {
	local rel="$1"
	local src="${GOLDEN_DIR}/${rel}"
	local fixture_path="/${rel}"
	local is_invalid=0 directives_mode=off
	local base name dir diag_path diag_status fmt_status
	[[ "${fixture_path}" == *"/invalid/"* ]] && is_invalid=1
	[[ "${fixture_path}" == *"/directives/"* ]] && directives_mode=collect
	base="$(basename "${src}")"
	name="${base%.sg}"
	dir="$(dirname "${src}")"
	diag_path="${dir}/${name}.diag"

	if "${SURGE_BIN}" diag --format short --directives="${directives_mode}" "${src}" > "${diag_path}" 2>/dev/null; then
		diag_status=0
	else
		diag_status=$?
	fi
	if [[ "${is_invalid}" -eq 1 ]]; then
		if [[ -n "${EXPECT_DIAG_ZERO[${rel}]+present}" ]]; then
			SEEN_DIAG_ZERO["${rel}"]=1
			if [[ "${diag_status}" -ne 0 ]]; then
				record_error "expected zero-status diagnostics became failing: ${rel}"
			fi
		elif [[ "${diag_status}" -ne 1 ]]; then
			record_error "invalid diagnostics status ${diag_status}, want 1: ${rel}"
		fi
		if [[ ! -s "${diag_path}" ]]; then
			record_error "diagnostics missing for invalid case: ${rel}"
		fi
	elif [[ "${diag_status}" -ne 0 ]]; then
		record_error "diagnostics failed for valid case: ${rel}"
	fi

	run_required "tokenize" "${dir}/${name}.tokens" "${SURGE_BIN}" tokenize "${src}"
	run_required "parse" "${dir}/${name}.ast" "${SURGE_BIN}" parse "${src}"

	if "${SURGE_BIN}" fmt --stdout "${src}" > "${dir}/${name}.fmt" 2>/dev/null; then
		fmt_status=0
	else
		fmt_status=$?
	fi
	if [[ -n "${EXPECT_FMT_FAILURE[${rel}]+present}" ]]; then
		SEEN_FMT_FAILURE["${rel}"]=1
		if [[ "${fmt_status}" -eq 0 ]]; then
			record_error "expected formatter failure became successful: ${rel}"
		elif [[ "${fmt_status}" -ne 1 ]]; then
			record_error "formatter failure status ${fmt_status}, want 1: ${rel}"
		elif ! cp "${src}" "${dir}/${name}.fmt"; then
			record_error "failed to copy formatter fallback: ${rel}"
		fi
	elif [[ "${fmt_status}" -ne 0 ]]; then
		record_error "unlisted formatter failure: ${rel}"
	fi

	if [[ "${fixture_path}" == *"/hir/"* ]]; then
		run_emit "hir" "HIR" "${rel}" "${dir}/${name}.hir" "${SURGE_BIN}" diag --format short --emit-hir "${src}"
	fi
	if [[ "${fixture_path}" == *"/hir_borrow/"* ]]; then
		run_emit "hir_borrow" "HIR borrow" "${rel}" "${dir}/${name}.hir" "${SURGE_BIN}" diag --format short --emit-hir --emit-borrow "${src}"
	fi
	if [[ "${fixture_path}" == *"/instantiations/"* ]]; then
		run_emit "instantiations" "instantiations" "${rel}" "${dir}/${name}.inst" "${SURGE_BIN}" diag --format short --emit-instantiations "${src}"
	fi
	if [[ "${fixture_path}" == *"/mono/"* ]]; then
		run_emit "mono" "monomorphization" "${rel}" "${dir}/${name}.mono" "${SURGE_BIN}" diag --format short --emit-mono "${src}"
	fi
	if [[ "${fixture_path}" == *"/mir/"* ]]; then
		run_emit "mir" "MIR" "${rel}" "${dir}/${name}.mir" "${SURGE_BIN}" diag --format short --emit-mir "${src}"
	fi
	return 0
}

# The keep-list is the corpus's INPUTS: the program, the recorded answer, and
# the sidecars that say how to ask for it. `.backends` names which backends a
# fixture is checked on; `.order-backends` names where its recorded interleaving
# is promised. Both are inputs like `.flags`, not something this script
# re-derives. Sweeping either away silently broadens the behavioural claim.
if ! (cd "${GOLDEN_DIR}" && find . ! -path './spec_audit/*' ! -path './crossing/crosses_deferred/*' -type f ! -name '*.sg' ! -name '*.script' ! -name '*.out' ! -name '*.code' ! -name '*.args' ! -name '*.stdin' ! -name '*.flags' ! -name '*.backends' ! -name '*.order-backends' -delete); then
	echo "failed to delete stale golden sidecars" >&2
	exit 1
fi
rm -rf "${CORE_GOLDEN_DIR}"
mkdir -p "${CORE_GOLDEN_DIR}"
cp -a "${ROOT_DIR}/core/." "${CORE_GOLDEN_DIR}/"
validate_corpus_tree "${GOLDEN_DIR}"
if ! (cd "${GOLDEN_DIR}" && find . ! -path './spec_audit/*' ! -path './crossing/crosses_deferred/*' -type f -name '*.sg' -print0 | sort -z) > "${SOURCES_FILE}"; then
	echo "failed to enumerate golden sources" >&2
	exit 1
fi

while IFS= read -r -d '' rel; do
	rel="${rel#./}"
	base="$(basename "${rel}")"
	if [[ "${base}" == _* ]]; then
		continue
	fi
	generate_outputs "${rel}"
done < "${SOURCES_FILE}"
for rel in "${DIAG_ZERO_PATHS[@]}"; do
	if [[ -z "${SEEN_DIAG_ZERO[${rel}]+present}" ]]; then
		record_error "stale zero-status diagnostic expectation: ${rel}"
	fi
done
for rel in "${FMT_FAILURE_PATHS[@]}"; do
	if [[ -z "${SEEN_FMT_FAILURE[${rel}]+present}" ]]; then
		record_error "stale formatter-failure expectation: ${rel}"
	fi
done
for emit_key in "${EMIT_FAILURE_KEYS[@]}"; do
	if [[ -z "${SEEN_EMIT_FAILURE[${emit_key}]+present}" ]]; then
		record_error "stale emit-failure expectation: ${emit_key}"
	fi
done
validate_corpus_tree "${GOLDEN_DIR}"
if [[ -s "${ERRORS_FILE}" ]]; then
	echo "Errors found during golden file generation:" >&2
	while IFS= read -r error; do
		printf '  %s\n' "${error}" >&2
	done < "${ERRORS_FILE}"
	exit 1
fi
cd "${ROOT_DIR}"
if ! mv "${LIVE_GOLDEN_DIR}" "${BACKUP_GOLDEN_DIR}"; then
	echo "failed to stage the live corpus for replacement" >&2
	exit 1
fi
if ! mv "${STAGED_GOLDEN_DIR}" "${LIVE_GOLDEN_DIR}"; then
	echo "failed to install generated golden corpus" >&2
	if ! mv "${BACKUP_GOLDEN_DIR}" "${LIVE_GOLDEN_DIR}"; then
		echo "failed to restore live golden corpus after install failure" >&2
	fi
	exit 1
fi
