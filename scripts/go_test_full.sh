#!/usr/bin/env bash
set -euo pipefail

go_bin=${1:-go}
package_timeout=${SURGE_PACKAGE_TIMEOUT:-300s}
integration_shards=${SURGE_INTEGRATION_TEST_SHARDS:-8}

if ! [[ ${integration_shards} =~ ^[1-9][0-9]*$ ]]; then
  echo "SURGE_INTEGRATION_TEST_SHARDS must be a positive integer" >&2
  exit 2
fi

mapfile -t ordinary_packages < <("${go_bin}" list ./... | grep -Ev '/internal/(driver|vm)$')
"${go_bin}" test "${ordinary_packages[@]}" --timeout "${package_timeout}"

run_sharded_package() {
  local package=$1
  local -a tests groups
  mapfile -t tests < <("${go_bin}" test "${package}" -list '^(Test|Example|Fuzz)' | grep -E '^(Test|Example|Fuzz)[[:alnum:]_]*$' | LC_ALL=C sort)
  if ((${#tests[@]} == 0)); then
    echo "no top-level tests discovered in ${package}" >&2
    return 2
  fi

  groups=()
  local index shard separator
  for index in "${!tests[@]}"; do
    shard=$((index % integration_shards))
    separator=''
    if [[ -n ${groups[shard]:-} ]]; then
      separator='|'
    fi
    groups[shard]="${groups[shard]:-}${separator}${tests[index]}"
  done

  local selected=0
  for shard in "${!groups[@]}"; do
    [[ -n ${groups[shard]} ]] || continue
    selected=$((selected + $(tr '|' '\n' <<<"${groups[shard]}" | wc -l)))
    echo ">> ${package} shard $((shard + 1))/${integration_shards}"
    "${go_bin}" test "${package}" -run "^(${groups[shard]})$" --timeout "${package_timeout}"
  done
  if ((selected != ${#tests[@]})); then
    echo "${package}: selected ${selected} tests, discovered ${#tests[@]}" >&2
    return 2
  fi
}

run_sharded_package ./internal/driver
run_sharded_package ./internal/vm
