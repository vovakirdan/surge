#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

GOLDEN_DIR="${ROOT_DIR}/testdata/golden"
SURGE_BIN="${SURGE_BIN:-${ROOT_DIR}/surge}"
CORE_GOLDEN_DIR="${GOLDEN_DIR}/core_stdlib"

# Set SURGE_STDLIB to use local stdlib during golden file generation
# Always use local stdlib for golden tests, ignore any pre-existing SURGE_STDLIB
export SURGE_STDLIB="${ROOT_DIR}"

# Очищаем старые артефакты перед регенерацией, чтобы убрать лишние файлы
# Keep debugger golden inputs/outputs.
find "${GOLDEN_DIR}" -path "${GOLDEN_DIR}/spec_audit" -prune -o -type f ! -name '*.sg' ! -name '*.script' ! -name '*.out' ! -name '*.code' ! -name '*.args' ! -name '*.stdin' ! -name '*.flags' -exec rm {} \;

# Sync stdlib core sources into golden tree so they are always covered by diagnostics.
rm -rf "${CORE_GOLDEN_DIR}"
mkdir -p "${CORE_GOLDEN_DIR}"
cp -a "${ROOT_DIR}/core/." "${CORE_GOLDEN_DIR}/"

if [[ ! -x "${SURGE_BIN}" ]]; then
	if command -v surge >/dev/null 2>&1; then
		SURGE_BIN="$(command -v surge)"
	else
		echo "surge binary not found. Build ./surge or set SURGE_BIN to the binary path." >&2
		exit 1
	fi
fi

generate_outputs() {
	local src="$1"
	local out_dir="$2"
	local is_invalid="$3"
	local copy_src="${4:-0}"
	local directives_mode="${5:-off}"
	local diag_path

	local base name dir
	base="$(basename "${src}")"
	name="${base%.sg}"
	dir="${out_dir}"

	if [[ "${copy_src}" -eq 1 ]]; then
		mkdir -p "${dir}"
		cp "${src}" "${dir}/${base}"
	fi

	diag_path="${dir}/${name}.diag"
	if ! "${SURGE_BIN}" diag --format short --directives="${directives_mode}" "${src}" > "${diag_path}" 2>/dev/null; then
		if [[ "${is_invalid}" -eq 0 ]]; then
			echo "diagnostics failed for valid case: ${src}" >&2
			exit 1
		fi
	fi

	if [[ "${is_invalid}" -eq 1 && ! -s "${diag_path}" ]]; then
		echo "diagnostics missing for invalid case: ${src}" >&2
		exit 1
	fi

	"${SURGE_BIN}" tokenize "${src}" > "${dir}/${name}.tokens" 2>/dev/null
	"${SURGE_BIN}" parse "${src}" > "${dir}/${name}.ast" 2>/dev/null

	if ! "${SURGE_BIN}" fmt --stdout "${src}" > "${dir}/${name}.fmt" 2>/dev/null; then
		cp "${src}" "${dir}/${name}.fmt"
		if [[ "${GOLDEN_VERBOSE:-0}" != "0" ]]; then
			echo "fmt failed for ${src}, copied original content" >&2
		fi
	fi

	# Generate HIR output for files in hir directory
	if [[ "${src}" == *"/hir/"* ]]; then
		"${SURGE_BIN}" diag --format short --emit-hir "${src}" > "${dir}/${name}.hir" 2>&1 || true
	fi

	# Generate HIR+borrow output for files in hir_borrow directory
	if [[ "${src}" == *"/hir_borrow/"* ]]; then
		"${SURGE_BIN}" diag --format short --emit-hir --emit-borrow "${src}" > "${dir}/${name}.hir" 2>&1 || true
	fi

	# Generate instantiation map output for files in instantiations directory
	if [[ "${src}" == *"/instantiations/"* ]]; then
		"${SURGE_BIN}" diag --format short --emit-instantiations "${src}" > "${dir}/${name}.inst" 2>&1 || true
	fi

	# Generate monomorphized output for files in mono directory
	if [[ "${src}" == *"/mono/"* ]]; then
		"${SURGE_BIN}" diag --format short --emit-mono "${src}" > "${dir}/${name}.mono" 2>&1 || true
	fi

	# Generate MIR output for files in mir directory
	if [[ "${src}" == *"/mir/"* ]]; then
		"${SURGE_BIN}" diag --format short --emit-mir "${src}" > "${dir}/${name}.mir" 2>&1 || true
	fi
}

find "${GOLDEN_DIR}" -path "${GOLDEN_DIR}/spec_audit" -prune -o -type f -name '*.sg' -print0 | sort -z | while IFS= read -r -d '' src; do
	base="$(basename "${src}")"
	if [[ "${base}" == _* ]]; then
		continue
	fi

	dir="$(dirname "${src}")"
	is_invalid=0
	if [[ "${src}" == *"/invalid/"* ]]; then
		is_invalid=1
	fi

	# Use --directives=collect for directive test directories
	directives_mode="off"
	if [[ "${src}" == *"/directives/"* ]]; then
		directives_mode="collect"
	fi

	generate_outputs "${src}" "${dir}" "${is_invalid}" 0 "${directives_mode}"
done
