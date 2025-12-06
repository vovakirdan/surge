#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

GOLDEN_DIR="${ROOT_DIR}/testdata/golden"
SURGE_BIN="${SURGE_BIN:-${ROOT_DIR}/surge}"
CORE_GOLDEN_DIR="${GOLDEN_DIR}/stdlib_core/core"

# Set SURGE_STDLIB to use local stdlib during golden file generation
# Always use local stdlib for golden tests, ignore any pre-existing SURGE_STDLIB
export SURGE_STDLIB="${ROOT_DIR}"

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

	local base name dir
	base="$(basename "${src}")"
	name="${base%.sg}"
	dir="${out_dir}"

	if [[ "${copy_src}" -eq 1 ]]; then
		mkdir -p "${dir}"
		cp "${src}" "${dir}/${base}"
	fi

	if ! "${SURGE_BIN}" diag --format short "${src}" > "${dir}/${name}.diag" 2>/dev/null; then
		if [[ "${is_invalid}" -eq 0 ]]; then
			echo "diagnostics failed for valid case: ${src}" >&2
			exit 1
		fi
	fi

	"${SURGE_BIN}" tokenize "${src}" > "${dir}/${name}.tokens" 2>/dev/null
	"${SURGE_BIN}" parse "${src}" > "${dir}/${name}.ast" 2>/dev/null

	if ! "${SURGE_BIN}" fmt --stdout "${src}" > "${dir}/${name}.fmt" 2>/dev/null; then
		cp "${src}" "${dir}/${name}.fmt"
		if [[ "${GOLDEN_VERBOSE:-0}" != "0" ]]; then
			echo "fmt failed for ${src}, copied original content" >&2
		fi
	fi
}

find "${GOLDEN_DIR}" -type f -name '*.sg' -print0 | sort -z | while IFS= read -r -d '' src; do
	base="$(basename "${src}")"
	if [[ "${base}" == _* ]]; then
		continue
	fi
	if [[ "${src}" == "${CORE_GOLDEN_DIR}"/* ]]; then
		continue
	fi

	dir="$(dirname "${src}")"
	is_invalid=0
	if [[ "${src}" == *"/invalid/"* ]]; then
		is_invalid=1
	fi

	generate_outputs "${src}" "${dir}" "${is_invalid}" 0
done

# Core stdlib files are validated via testdata/golden/stdlib_core/* instead
# (direct diagnosis of core/* is forbidden due to reserved namespace)
if [[ -d "${CORE_GOLDEN_DIR}" ]]; then
	if ! "${SURGE_BIN}" diag --format short "${CORE_GOLDEN_DIR}" >/dev/null 2>&1; then
		echo "stdlib_core/core diagnostics failed" >&2
		exit 1
	fi
fi
