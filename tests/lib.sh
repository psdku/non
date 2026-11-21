#!/usr/bin/env bash
# Shared harness for the TUI tests.
#
# These tests drive the real binary inside tmux and assert on what the terminal
# actually shows. They are slower and coarser than the Go tests, and they catch
# the class of bug the Go tests cannot: a key that does not arrive, a view that
# does not repaint, a message that is drawn but never cleared.
#
# Usage from a test file:
#
#   . "$(dirname "$0")/lib.sh"
#   start                       # builds non, makes a scratch collection
#   send m                      # type a key
#   expect "edit metadata"      # wait for the screen to contain a string
#   expect_not "saved"
#   finish                      # kill the session, report, exit non-zero on faiure
#
# The session uses its own tmux socket (-L nondev): killing the default server
# would kill the user's own sessions.

set -uo pipefail

# --- configuration -------------------------------------------------------

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$TESTS_DIR/.." && pwd)"
SOCKET="${NON_TEST_SOCKET:-nondev}"
SESSION="${NON_TEST_SESSION:-nontest}"
COLS="${NON_TEST_COLS:-100}"
ROWS="${NON_TEST_ROWS:-28}"

NON="${NON_TEST_BIN:-$TESTS_DIR/../non}"

# Counters, exported for finish.
failures=0
checks=0
current_test=""

# --- output --------------------------------------------------------------

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	red=$'\033[31m'
	green=$'\033[32m'
	yellow=$'\033[33m'
	dim=$'\033[2m'
	bold=$'\033[1m'
	off=$'\033[0m'
else
	red=; green=; yellow=; dim=; bold=; off=
fi

info()  { printf '%s\n' "${dim}# $*${off}"; }
pass()  { printf '%s\n' "${green}ok${off}   $*"; }
fail()  {
	failures=$((failures + 1))
	printf '%s\n' "${red}FAIL${off} $*"
	if [ -n "${KEEP_SCREEN:-}" ]; then
		printf '%s\n' "${dim}--- screen ---${off}"
		capture | sed 's/^/    /'
		printf '%s\n' "${dim}--- end ---${off}"
	fi
}
die()   { printf '%s\n' "${red}error${off} $*" >&2; exit 2; }

# --- lifecycle -----------------------------------------------------------

# build compiles the binary the tests drive, so they never test a stale build.
build() {
	info "building non"
	( cd "$REPO_DIR" && go build -o "$NON" . ) || die "go build failed"
}

# start makes a fresh collection in a temp dir and opens the TUI on it.
#
# Arguments are file names to create (as tiny PDFs with the real names the
# extraction fixtures use are not needed: the tests set metadata directly).
start() {
	build
	new_work

	for name in "$@"; do
		printf '%%PDF-1.4\n' > "$WORK/$name"
	done

	launch

	# Wait for the first paint. "loading…" is the only view before
	# WindowSizeMsg arrives, so its absence means the model is ready.
	wait_for_gone "loading"
	sleep 0.05
}

# launch starts the TUI in $WORK.
#
# Two things are pinned deliberately. The directory is passed as ".", because
# without an argument non opens the configured default directory -- the user's
# real collection -- and no test may touch that. XDG_CONFIG_HOME points into the
# scratch dir, so the user's global config cannot change what a test sees.
launch() {
	export XDG_CONFIG_HOME="$WORK/config"
	mkdir -p "$XDG_CONFIG_HOME"
	tmux -L "$SOCKET" kill-server 2>/dev/null
	tmux -L "$SOCKET" new-session -d -s "$SESSION" -x "$COLS" -y "$ROWS" \
		"cd '$WORK' && '$NON' ." || die "tmux failed to start"
	wait_for_gone "loading"
	sleep 0.05
}

# start_with_store writes a .non.json before opening the TUI.
start_with_store() {
	local store="$1"
	shift
	build
	new_work
	for name in "$@"; do
		printf '%%PDF-1.4\n' > "$WORK/$name"
	done
	printf '%s\n' "$store" > "$WORK/.non.json"
	launch
}

# WORK_DIRS records every scratch directory this test made. A test that restarts
# the TUI (to change the collection) calls start twice, and a single $WORK
# variable would forget the first one and leak it.
WORK_DIRS=()

cleanup() {
	tmux -L "$SOCKET" kill-server 2>/dev/null
	local d
	for d in "${WORK_DIRS[@]}"; do
		if [ -n "${KEEP_WORK:-}" ]; then
			info "kept $d"
		else
			rm -rf "$d"
		fi
	done
}

# new_work makes a scratch directory and remembers it.
new_work() {
	WORK="$(mktemp -d "${TMPDIR:-/tmp}/non-test-XXXXXX")"
	WORK_DIRS+=("$WORK")
	if [ "${#WORK_DIRS[@]}" -eq 1 ]; then
		trap cleanup EXIT
	fi
}

# finish reports the tally and exits non-zero if anything failed.
finish() {
	if [ "$failures" -eq 0 ]; then
		printf '%s\n' "${green}${checks} checks passed${off}"
		exit 0
	fi
	printf '%s\n' "${red}${failures} of ${checks} checks failed${off}"
	exit 1
}

# --- driving the UI ------------------------------------------------------

# capture prints the current screen with styling stripped, which is what a
# human reads.
capture() {
	tmux -L "$SOCKET" capture-pane -p -t "$SESSION"
}

# capture_color prints the screen with escape sequences, for the rare test that
# must assert on a colour or on the reverse-video cursor.
capture_color() {
	tmux -L "$SOCKET" capture-pane -p -e -t "$SESSION"
}

# send types one key by NAME ("m", "Enter", "Tab", "Space", "Escape", "Down"),
# not a literal string: send-keys would otherwise type the letters of "Enter".
send() {
	tmux -L "$SOCKET" send-keys -t "$SESSION" "$@"
	sleep 0.15
}

# type_text types literal characters, so a space or a quote reaches the app.
type_text() {
	tmux -L "$SOCKET" send-keys -l -t "$SESSION" "$1"
	sleep 0.15
}

# clear_input empties a pre-filled prompt. A rename box starts with the current
# name in it, so a test that types a new name must clear it first or the text is
# appended to the old one.
clear_input() {
	tmux -L "$SOCKET" send-keys -t "$SESSION" C-u
	sleep 0.15
}

# check increments the counter; it is what every expect* uses.
check() { checks=$((checks + 1)); }

# expect asserts that the screen contains a fixed string.
expect() {
	check
	if capture | grep -qF -- "$1"; then
		pass "screen shows: $1"
	else
		fail "screen does not show: $1"
		return 1
	fi
}

# expect_not asserts the string is absent.
expect_not() {
	check
	if capture | grep -qF -- "$1"; then
		fail "screen should not show: $1"
		return 1
	else
		pass "screen does not show: $1"
	fi
}

# expect_re matches a regular expression against the screen.
expect_re() {
	check
	if capture | grep -qE -- "$1"; then
		pass "screen matches: $1"
	else
		fail "screen does not match: $1"
		return 1
	fi
}

# expect_line asserts that some line contains the string AND the line is exactly
# the terminal width: the layout must not be ragged or overflow.
expect_line() {
	check
	local found
	found="$(capture | grep -F -- "$1" | head -1)"
	if [ -z "$found" ]; then
		fail "no line contains: $1"
		return 1
	fi
	local w
	w="$(printf '%s' "$found" | wc -m)"
	if [ "$w" -ne "$COLS" ]; then
		fail "line with $1 is $w cells wide, want $COLS"
		return 1
	fi
	pass "line with $1 is $COLS cells wide"
}

# expect_reverse asserts that the screen uses reverse video somewhere, which is
# how the list cursor is drawn.
expect_reverse() {
	check
	if capture_color | grep -q $'\033\[7m'; then
		pass "a reverse-video cursor is drawn"
	else
		fail "no reverse-video cursor on screen"
	fi
}

# wait_for_gone waits until a string disappears from the screen.
wait_for_gone() {
	local i
	for i in $(seq 1 100); do
		if ! capture | grep -qF -- "$1"; then
			return 0
		fi
		sleep 0.05
	done
	return 1
}

# press_until sends a key until the screen shows a string (at most n times).
#
# Navigating a wrap-around list by counting keystrokes is brittle: the row count
# changes whenever a setting is added. Pressing until the cursor lands where the
# test wants it is stable, and it fails loudly if it never arrives.
press_until() {
	local key="$1" want="$2" n="${3:-10}" i
	for i in $(seq 1 "$n"); do
		if capture | grep -qF -- "$want"; then
			return 0
		fi
		send "$key"
	done
	capture | grep -qF -- "$want"
}

# --- assertions with an explicit condition ---

# ok reports a check whose condition the caller already evaluated.
ok() {
	check
	if [ "$1" -eq 0 ]; then pass "$2"; else fail "$2"; fi
}

# --- files ---------------------------------------------------------------

# store prints the collection's .non.json.
store() { cat "$WORK/.non.json"; }

# file_exists asserts a file is in the collection.
file_exists() {
	check
	if [ -f "$WORK/$1" ]; then
		pass "file exists: $1"
	else
		fail "missing file: $1"
	fi
}

file_absent() {
	check
	if [ -f "$WORK/$1" ]; then
		fail "file should be gone: $1"
	else
		pass "file is gone: $1"
	fi
}

# store_has asserts the metadata file contains a string.
store_has() {
	check
	if store | grep -qF -- "$1"; then
		pass "store has: $1"
	else
		fail "store is missing: $1"
	fi
}

store_lacks() {
	check
	if store | grep -qF -- "$1"; then
		fail "store should not have: $1"
	else
		pass "store lacks: $1"
	fi
}
