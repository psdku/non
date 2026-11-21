#!/usr/bin/env bash
# The action menu and confirmation flow: enter opens the menu, and a rename that
# would lose characters asks first.
#
# The confirmation is the part no Go test sees properly: it is a modal whose
# whole purpose is to stop a keystroke, and the only way to trust it is to press
# the keys.

. "$(dirname "$0")/lib.sh"

start "a paper.pdf"

# Enter opens the action menu on the selected row.
send Enter
expect "actions"
expect "Rename"
expect "Inspect"
# Esc closes it without choosing anything.
send Escape
expect_not "Edit metadata"

# r opens the rename prompt, pre-filled with the current name. A space is
# normalized to a dash, which is a change, so it asks first.
send r
expect "rename: "
clear_input
type_text "clean name"
send Enter
expect "name normalized to clean-name.pdf"
# Declining leaves the file alone.
send n
file_exists "a paper.pdf"
file_absent "clean-name.pdf"
# Accepting performs it.
send r
clear_input
type_text "clean name"
send Enter
send y
file_exists "clean-name.pdf"
file_absent "a paper.pdf"

# A name with a character the normalizer removes says what it will do, and does
# nothing until the dialog is answered. A quote is removed; "?" is legal in a
# file name and is kept.
send r
clear_input
type_text 'what"name'
send Enter
expect "removed quotes"
expect "using whatname.pdf"
file_exists "clean-name.pdf"
send n
file_exists "clean-name.pdf"
# Enter accepts, which is what makes "yes" a deliberate act.
send r
clear_input
type_text 'what"name'
send Enter
send Enter
file_exists "whatname.pdf"

finish
