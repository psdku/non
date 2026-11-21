#!/usr/bin/env bash
# The metadata form: open it, edit, save, and check the file.
#
# This is the test that would have caught the phantom-cursor bug and the
# "message never clears" bug, because both are only visible on screen.

. "$(dirname "$0")/lib.sh"

start alpha.pdf beta.pdf

# The form opens with the title already filled from the file name.
send m
expect "edit metadata"
expect "alpha"

# The cursor is one reverse-video bar, and only one: the phantom-cursor bug drew
# a second one in the field that had just been left.
send Tab
check
bars="$(capture_color | grep -o $'\033\[7m' | wc -l)"
if [ "$bars" -eq 1 ]; then
	pass "exactly one cursor after moving"
else
	fail "$bars cursors on screen after moving"
fi

# The cursor is now in the author field (the second row), so typing lands there.
type_text "Test Author"

# Save with ctrl+s and check the file, not just the screen: the screen can show
# "saved" while the disk says otherwise.
send C-s
expect "metadata saved"
store_has '"author": "Test Author"'
store_has '"title": "alpha"'

# The list row now shows the title in the details pane, not the file name.
expect "Test Author"

# Reopening shows what was saved, so the round trip through the form works.
send m
expect "Test Author"
send Escape

finish
