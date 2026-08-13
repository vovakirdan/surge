#!/usr/bin/env bash
set -euo pipefail

readonly LIMIT=500

# Files this gate deliberately does not size, each for a reason that outranks the
# limit. Keep the list SHORT and keep the reason next to the entry: an exemption
# with no stated reason is indistinguishable from one added to make a red gate
# go away.
#
# internal/diag/codes.go — the single registry of diagnostic numbers. Splitting
# it is what caused NINE numbers to be taken twice: a lane adding its own
# companion file cannot see the numbers another lane is taking in a file that
# does not exist on its branch. So the owner ruled that a code is declared here
# and nowhere else, and that this gate makes room for the registry rather than
# the registry making room for the gate. The hazard that motivated one-file-only
# is now caught mechanically by internal/diag's uniqueness test, and the size
# signal is not lost either — check_file_sizes.sh still rates this file on every
# `make check`. What is exempted is only the growth VIOLATION, and only here.
readonly -a SIZE_EXEMPT_PATHS=(
    "internal/diag/codes.go"
)

size_exempt() {
    local candidate=$1 exempt
    for exempt in "${SIZE_EXEMPT_PATHS[@]}"; do
        [[ "$candidate" == "$exempt" ]] && return 0
    done
    return 1
}

clear_repo_local_git_env() {
    local name

    unset GIT_ALTERNATE_OBJECT_DIRECTORIES \
        GIT_CEILING_DIRECTORIES \
        GIT_COMMON_DIR \
        GIT_CONFIG \
        GIT_CONFIG_COUNT \
        GIT_CONFIG_PARAMETERS \
        GIT_DIR \
        GIT_DISCOVERY_ACROSS_FILESYSTEM \
        GIT_GRAFT_FILE \
        GIT_IMPLICIT_WORK_TREE \
        GIT_INDEX_FILE \
        GIT_INTERNAL_SUPER_PREFIX \
        GIT_NAMESPACE \
        GIT_NO_REPLACE_OBJECTS \
        GIT_OBJECT_DIRECTORY \
        GIT_PREFIX \
        GIT_QUARANTINE_PATH \
        GIT_REPLACE_REF_BASE \
        GIT_SHALLOW_FILE \
        GIT_WORK_TREE

    for name in "${!GIT_CONFIG_KEY_@}" "${!GIT_CONFIG_VALUE_@}"; do
        unset "$name"
    done
}

die() {
    printf 'runtime-v2-file-size-check: error: %s\n' "$*" >&2
    exit 2
}

is_source_path() {
    case "$1" in
        *.go|*.c|*.h) return 0 ;;
        *) return 1 ;;
    esac
}

write_blob() {
    local oid=$1
    local destination=$2
    local label=$3
    if [[ -z "$oid" || "$oid" =~ ^0+$ ]]; then
        : >"$destination"
        return
    fi
    git cat-file blob "$oid" >"$destination" || die "cannot read committed blob for $label"
}

line_count() {
    awk 'END { print NR + 0 }' "$1"
}

line_churn() {
    local before=$1
    local after=$2
    local output rc added deleted ignored
    set +e
    output=$(git -c diff.algorithm=myers diff --no-index --no-renames \
        --no-ext-diff --no-textconv --no-indent-heuristic --numstat -- \
        "$before" "$after" 2>/dev/null)
    rc=$?
    set -e
    if (( rc > 1 )); then
        die "cannot calculate committed line churn"
    fi
    if [[ -z "$output" ]]; then
        printf '0\n'
        return
    fi
    IFS=$'\t' read -r added deleted ignored <<<"$output"
    if [[ ! "$added" =~ ^[0-9]+$ || ! "$deleted" =~ ^[0-9]+$ ]]; then
        die "binary content is not valid source for this gate"
    fi
    printf '%d\n' "$((added + deleted))"
}

violation() {
    local code=$1
    local path=$2
    shift 2
    local shown
    printf -v shown '%q' "$path"
    if size_exempt "$path"; then
        # Reported, not counted. A silent exemption would let the file grow
        # without anyone seeing it happen; this way the growth is still on the
        # record of every run.
        printf 'EXEMPT code=%s path=%s %s\n' "$code" "$shown" "$*" >&2
        return
    fi
    printf 'VIOLATION code=%s path=%s %s\n' "$code" "$shown" "$*" >&2
    violations=$((violations + 1))
}

clear_repo_local_git_env
command -v git >/dev/null 2>&1 || die "git is required"
[[ -n "${EPIC_BASE:-}" ]] || die \
    "EPIC_BASE is required; fix: make runtime-v2-file-size-check EPIC_BASE=<ancestor-commit>"

repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || die "run inside a git repository"
cd "$repo_root"
readonly effective_awk="$repo_root/scripts/effective_loc.awk"
[[ -r "$effective_awk" ]] || die "missing scripts/effective_loc.awk"

base_oid=$(git rev-parse --verify --quiet --end-of-options "${EPIC_BASE}^{commit}" 2>/dev/null) ||
    die "EPIC_BASE does not name a commit: $EPIC_BASE"
head_oid=$(git rev-parse --verify HEAD^{commit} 2>/dev/null) || die "HEAD does not name a commit"
git merge-base --is-ancestor "$base_oid" "$head_oid" 2>/dev/null ||
    die "EPIC_BASE is not an ancestor of HEAD; fix: choose the epic's committed fork point"

tmp_dir=$(mktemp -d) || die "cannot create temporary directory"
trap 'rm -rf -- "$tmp_dir"' EXIT
raw_diff="$tmp_dir/diff.raw"
git -c diff.renameLimit=999999 diff --raw -z --find-renames=50% --find-copies=50% --no-abbrev \
    "$base_oid" "$head_oid" -- >"$raw_diff" || die "cannot read committed BASE..HEAD diff"

printf 'runtime-v2-file-size-check: base=%s head=%s limit=%d-effective-LOC\n' \
    "$base_oid" "$head_oid" "$LIMIT"
printf 'runtime-v2-file-size-check: worktree state ignored; measuring committed blobs only\n'

files=0
violations=0
while IFS= read -r -d '' metadata; do
    [[ "$metadata" == :* ]] || die "malformed raw git diff metadata"
    IFS=' ' read -r old_mode new_mode old_oid new_oid status <<<"${metadata#:}"
    [[ -n "$status" ]] || die "raw git diff record has no status"
    code=${status:0:1}
    IFS= read -r -d '' first_path || die "raw git diff record has no path"
    old_path=$first_path
    new_path=$first_path
    case "$code" in
        A) old_path= ;;
        D) new_path= ;;
        R|C)
            IFS= read -r -d '' new_path || die "rename/copy record has no destination path"
            ;;
        M|T) ;;
        *)
            if is_source_path "$first_path"; then
                die "unsupported committed git status $status for source path"
            fi
            continue
            ;;
    esac

    old_scoped=0
    new_scoped=0
    [[ -n "$old_path" ]] && is_source_path "$old_path" && old_scoped=1
    [[ -n "$new_path" ]] && is_source_path "$new_path" && new_scoped=1
    [[ "$code" == C ]] && (( ! new_scoped )) && continue
    (( old_scoped || new_scoped )) || continue

    is_new=0
    is_deleted=0
    if (( ! old_scoped )) || [[ "$code" == A || "$code" == C ]]; then
        old_oid=
        is_new=1
    fi
    if (( ! new_scoped )) || [[ "$code" == D ]]; then
        new_oid=
        is_deleted=1
    fi
    report_path=$new_path
    (( new_scoped )) || report_path=$old_path

    write_blob "$old_oid" "$tmp_dir/base.source" "$old_path"
    write_blob "$new_oid" "$tmp_dir/head.source" "$new_path"
    awk -v mode=lines -f "$effective_awk" "$tmp_dir/base.source" >"$tmp_dir/base.effective" ||
        die "effective LOC parser failed for committed base blob"
    awk -v mode=lines -f "$effective_awk" "$tmp_dir/head.source" >"$tmp_dir/head.effective" ||
        die "effective LOC parser failed for committed head blob"

    physical_base=$(line_count "$tmp_dir/base.source")
    physical_head=$(line_count "$tmp_dir/head.source")
    physical_churn=$(line_churn "$tmp_dir/base.source" "$tmp_dir/head.source")
    effective_base=$(line_count "$tmp_dir/base.effective")
    effective_head=$(line_count "$tmp_dir/head.effective")
    effective_churn=$(line_churn "$tmp_dir/base.effective" "$tmp_dir/head.effective")
    printf -v shown_path '%q' "$report_path"
    printf 'FILE status=%s path=%s physical_base=%d physical_head=%d physical_churn=%d effective_base=%d effective_head=%d effective_churn=%d\n' \
        "$status" "$shown_path" "$physical_base" "$physical_head" "$physical_churn" \
        "$effective_base" "$effective_head" "$effective_churn"
    files=$((files + 1))

    if (( is_new && effective_head > LIMIT )); then
        violation NEW_OVER_500 "$report_path" \
            "effective_head=$effective_head limit=$LIMIT fix=split-or-extract-before-merge"
    fi
    if (( ! is_new && ! is_deleted && effective_base > LIMIT && effective_head > effective_base )); then
        violation LEGACY_GROWTH "$report_path" \
            "effective_base=$effective_base effective_head=$effective_head fix=do-not-grow-legacy-file"
    fi
    if (( ! is_new && ! is_deleted && effective_base <= LIMIT && effective_head > LIMIT )); then
        violation CROSSED_500 "$report_path" \
            "effective_base=$effective_base effective_head=$effective_head limit=$LIMIT fix=keep-head-at-or-below-limit"
    fi
    if (( ! is_new && ! is_deleted && effective_base > 0 &&
          2 * effective_churn >= effective_base && effective_head > LIMIT )); then
        violation REWRITE_OVER_500 "$report_path" \
            "effective_base=$effective_base effective_head=$effective_head effective_churn=$effective_churn fix=finish-the-rewrite-below-limit"
    fi
done <"$raw_diff"

if (( violations > 0 )); then
    printf 'runtime-v2-file-size-check: FAIL files=%d violations=%d\n' "$files" "$violations" >&2
    exit 1
fi
printf 'runtime-v2-file-size-check: PASS files=%d violations=0\n' "$files"
