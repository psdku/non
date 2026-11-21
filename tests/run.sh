#!/usr/bin/env bash
# Run every TUI test and summarize.
#
# Each test is a standalone script that can also be run on its own:
#
#   tests/run.sh              # all of them
#   tests/run.sh 04           # the ones whose name matches "04"
#
# Exit status is 0 only when every test passed.

set -uo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
filter="${1:-}"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	red=$'\033[31m'; green=$'\033[32m'; bold=$'\033[1m'; dim=$'\033[2m'; off=$'\033[0m'
else
	red=; green=; bold=; dim=; off=
fi

failed=()
total=0
for t in "$DIR"/[0-9]*.sh; do
	[ -x "$t" ] || continue
	name="$(basename "$t" .sh)"
	if [ -n "$filter" ] && [[ "$name" != *"$filter"* ]]; then
		continue
	fi
	total=$((total + 1))
	printf '\n%s\n' "${bold}== $name${off}"
	start=$(date +%s%N)
	if bash "$t"; then
		ms=$(( ($(date +%s%N) - start) / 1000000 ))
		printf '%s\n' "${dim}(${ms} ms)${off}"
	else
		failed+=("$name")
	fi
done

printf '\n'
if [ "$total" -eq 0 ]; then
	printf '%s\n' "${red}no tests matched '$filter'${off}"
	exit 2
fi
if [ "${#failed[@]}" -eq 0 ]; then
	printf '%s\n' "${green}${bold}all $total TUI tests passed${off}"
	exit 0
fi
printf '%s\n' "${red}${bold}${#failed[@]} of $total TUI tests failed:${off}"
for f in "${failed[@]}"; do
	printf '  %s\n' "$f"
done
exit 1
