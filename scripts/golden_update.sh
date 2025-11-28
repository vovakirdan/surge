#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

GOLDEN_DIR="${ROOT_DIR}/testdata/golden"
SURGE_BIN="${SURGE_BIN:-${ROOT_DIR}/surge}"
CORE_GOLDEN_DIR="${GOLDEN_DIR}/stdlib_core/core"

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

# For stdlib core modules: do NOT recreate (overwrite sources), just make sure diagnostics are empty,
# and bake tokens/ast/fmt for regression tracking.
find "${ROOT_DIR}/core" -maxdepth 1 -type f -name '*.sg' -print0 | sort -z | while IFS= read -r -d '' src; do
	abs_src="$(cd "$(dirname "${src}")" && pwd)/$(basename "${src}")"
	base="$(basename "${src}")"
	name="${base%.sg}"
	out_dir="${CORE_GOLDEN_DIR}"

	diag_file="${out_dir}/${name}.diag"

	mkdir -p "${out_dir}"

	# Check diagnostics: if non-empty, this is an error (must be valid).
	if ! "${SURGE_BIN}" diag --format short "${abs_src}" > "${diag_file}" 2>/dev/null; then
		echo "diagnostics failed for core stdlib file (should be valid): ${abs_src}" >&2
		exit 1
	fi
	if [[ -s "${diag_file}" ]]; then
		echo "non-empty diagnostics for core stdlib file (should be valid): ${abs_src}" >&2
		echo "==== diag output ===="
		cat "${diag_file}" >&2
		echo "==== end diag output ===="
		exit 1
	fi

	"${SURGE_BIN}" tokenize "${abs_src}" > "${out_dir}/${name}.tokens" 2>/dev/null
	"${SURGE_BIN}" parse "${abs_src}" > "${out_dir}/${name}.ast" 2>/dev/null

	if ! "${SURGE_BIN}" fmt --stdout "${abs_src}" > "${out_dir}/${name}.fmt" 2>/dev/null; then
		cp "${abs_src}" "${out_dir}/${name}.fmt"
		if [[ "${GOLDEN_VERBOSE:-0}" != "0" ]]; then
			echo "fmt failed for ${abs_src}, copied original content" >&2
		fi
	fi

	# Do NOT overwrite/copy the source file itself here (don't recreate the .sg in golden/core).
done
