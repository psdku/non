#!/usr/bin/env bash
# The settings modal: it floats over a dimmed screen, its message clears on the
# next key, and a change reaches the file.
#
# Both behaviours asserted here were once bugs: the modal used to clear the whole
# screen behind it, and its "saved" message used to stay until the box was closed.

. "$(dirname "$0")/lib.sh"

start alpha.pdf beta.pdf

send s
expect "settings"
expect "Open editor"
expect "Manage tags"
# The browser is still visible behind the box: the first row of the list is on
# screen, which is what "floats" means here.
expect "alpha.pdf"

# Toggling the first setting writes a message and the file.
store_before="$(store)"
send Space
expect "saved"
check
if [ "$(store)" != "$store_before" ]; then
	pass "toggling a setting wrote the file"
else
	fail "toggling a setting did not write the file"
fi
store_has "open_editor"

# Any keypress clears it, and the keymap comes back. This is the bug that was
# reported: the message stayed until another action or closing the modal.
send j
expect_not "saved"
expect "space toggle"

# The tag page is reachable. The cursor wraps, so press up until it is on the
# last row rather than counting keystrokes.
check
if press_until k "› Manage tags"; then
	pass "k reached Manage tags"
else
	fail "k never reached Manage tags"
fi
send Enter
expect "settings › tags"
expect "no tags yet"
# "a" opens the tag form. Enter moves to the next field, and ctrl+s saves; the
# list comes back with the new tag in it.
send a
type_text "LLM"
send Enter
type_text "large language models"
send C-s
expect "saved tag LLM"
expect "large language models"
# A duplicate is refused with a message, and the list is not damaged.
send a
type_text "llm"
send Enter
type_text "same tag, other case"
send C-s
expect "already exists"
# Esc leaves the form and the page, back to the browser.
send Escape
send Escape
expect "NORMAL"
# The box is gone: its top border is the thing to look for, since "settings"
# also appears in the status bar keymap.
expect_not "─── settings"
expect_not "settings › tags"

finish
