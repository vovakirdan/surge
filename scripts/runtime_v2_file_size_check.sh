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

usage() {
    cat <<'HELP'
usage: runtime_v2_file_size_check.sh [--worktree | --committed]

Sizes every source file the epic touches, in effective LOC, against EPIC_BASE.

  --worktree   size the files as they are ON DISK, including files that are
               staged or not added yet. This is the default, so the answer
               arrives while the growth can still be undone.
  --committed  size the committed blobs at HEAD and ignore the worktree. This
               is what the gate and CI run: the same input every time.

EPIC_BASE=<ancestor-commit> is required. SIZE_CHECK_SOURCE=worktree|committed
picks the same two modes, and an explicit argument outranks the variable.
HELP
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

write_worktree() {
    local path=$1
    local destination=$2
    # No path, or nothing on disk under it, is a deletion: the head side of the
    # comparison is empty, exactly as an absent blob is.
    if [[ -z "$path" || ! -f "$path" ]]; then
        : >"$destination"
        return
    fi
    cat -- "$path" >"$destination" || die "cannot read worktree file $path"
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
        die "cannot calculate line churn"
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

source_mode=${SIZE_CHECK_SOURCE:-worktree}
while (( $# )); do
    case "$1" in
        --worktree) source_mode=worktree ;;
        --committed) source_mode=committed ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; die "unknown argument: $1" ;;
    esac
    shift
done
case "$source_mode" in
    worktree|committed) ;;
    *) die "SIZE_CHECK_SOURCE must be worktree or committed, not: $source_mode" ;;
esac

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
if [[ "$source_mode" == committed ]]; then
    head_label=$head_oid
    banner="measuring committed blobs only"
    git -c diff.renameLimit=999999 diff --raw -z --find-renames=50% --find-copies=50% --no-abbrev \
        "$base_oid" "$head_oid" -- >"$raw_diff" || die "cannot read committed BASE..HEAD diff"
else
    head_label=worktree
    banner="measuring the worktree against $base_oid; HEAD is $head_oid"
    # A base and NO second commit is how git is asked to compare a commit with
    # the files on disk. Untracked files sit outside that comparison, so they
    # are appended as additions: a source file written but not added yet is the
    # commonest way a new file first crosses the limit.
    git -c diff.renameLimit=999999 diff --raw -z --find-renames=50% --find-copies=50% --no-abbrev \
        "$base_oid" -- >"$raw_diff" || die "cannot read BASE..worktree diff"
    git ls-files --others --exclude-standard -z >"$tmp_dir/untracked" ||
        die "cannot list untracked worktree files"
    while IFS= read -r -d '' untracked_path; do
        is_source_path "$untracked_path" || continue
        printf ':000000 100644 %040d %040d A\0%s\0' 0 0 "$untracked_path" >>"$raw_diff"
    done <"$tmp_dir/untracked"
fi

printf 'runtime-v2-file-size-check: base=%s head=%s limit=%d-effective-LOC\n' \
    "$base_oid" "$head_label" "$LIMIT"
printf 'runtime-v2-file-size-check: %s\n' "$banner"

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
        U)
            # Only a worktree comparison can report an unmerged path, and its
            # content is half of two commits: measuring it answers nothing.
            is_source_path "$first_path" || continue
            die "unmerged path $first_path; fix: finish the merge before measuring the worktree"
            ;;
        *)
            if is_source_path "$first_path"; then
                die "unsupported git status $status for source path"
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
    head_path=$new_path
    if (( ! old_scoped )) || [[ "$code" == A || "$code" == C ]]; then
        old_oid=
        is_new=1
    fi
    if (( ! new_scoped )) || [[ "$code" == D ]]; then
        new_oid=
        head_path=
        is_deleted=1
    fi
    report_path=$new_path
    (( new_scoped )) || report_path=$old_path

    write_blob "$old_oid" "$tmp_dir/base.source" "$old_path"
    if [[ "$source_mode" == worktree ]]; then
        write_worktree "$head_path" "$tmp_dir/head.source"
    else
        write_blob "$new_oid" "$tmp_dir/head.source" "$new_path"
    fi
    awk -v mode=lines -f "$effective_awk" "$tmp_dir/base.source" >"$tmp_dir/base.effective" ||
        die "effective LOC parser failed for committed base blob"
    awk -v mode=lines -f "$effective_awk" "$tmp_dir/head.source" >"$tmp_dir/head.effective" ||
        die "effective LOC parser failed for the head-side source"

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
