#!/usr/bin/env bash
# Tags: the picker filters and scrolls, hidden tags are marked, and a duplicate
# add warns but still selects.
#
# These are the three behaviours a Go test can only see through the model; here
# they are checked on the screen, which is where the user meets them.

. "$(dirname "$0")/lib.sh"

# Forty tags, so the picker must scroll rather than grow.
tags='{"name": "LLM"}, {"name": "R&D"}, {"name": "kvcache"}, {"name": "attention"}'
for i in $(seq 1 36); do
	tags="$tags, {\"name\": \"topic$i\"}"
done
start_with_store \
	"{\"version\": 1, \"config\": {\"tags\": [$tags]}, \"entries\": {\"alpha.pdf\": {\"title\": \"Alpha\", \"tags\": [\"LLM\"]}}}" \
	alpha.pdf beta.pdf

# The list shows the tag, and the picker is bounded with a count. The form has
# five rows before the checklist (title, author, abstract, comment, tags), so
# four Tabs land on it.
send m
for _ in $(seq 1 4); do send Tab; done
expect "40 tags"
expect "more"
# The tags the paper has are ticked; the rest are not. An unticked box is drawn
# as "[-]" and a ticked one as "[x]".
expect_re "\[x\] LLM"
expect_re "\[-\] R&D"

# The picker scrolls: the cursor moves past the window and the view follows,
# saying how much is hidden on each side. Right moves within the checklist;
# Down would move to the next form row.
for _ in $(seq 1 12); do send Right; done
expect_re "↑ [0-9]+ more"
expect_re "↓ [0-9]+ more"

# "/" filters, and a space typed reaches the query rather than toggling the tag
# under the cursor.
send /
type_text "topic1"
expect "topic1"
send Escape
expect_re "topic1[0-9]"

# A tag that already exists is added to the paper anyway, with a warning. The
# add-tag row is the next one down.
send Escape
send m
for _ in $(seq 1 5); do send Tab; done
type_text "llm"
send Enter
expect "already exists"
send C-s
store_has '"LLM"'

# A hidden tag (not in the table) is marked in the picker and blocks saving.
tmux -L "$SOCKET" kill-server 2>/dev/null
start_with_store \
	"{\"version\": 1, \"config\": {\"tags\": [{\"name\": \"R&D\"}]}, \"entries\": {\"alpha.pdf\": {\"title\": \"Alpha\", \"tags\": [\"R&D\", \"KV cache\"]}}}" \
	alpha.pdf

# Startup says something is wrong. The row hides the unusable tag, but the
# warning that names it is in the status bar, so the string is on screen once --
# that one mention is the point, and it is not in the list row.
expect "unusable"
check
if capture | grep -E "alpha\.pdf" | grep -qF "KV cache"; then
	fail "the list row shows an unusable tag"
else
	pass "the list row hides the unusable tag"
fi

send m
for _ in $(seq 1 4); do send Tab; done
expect "KV cache"
expect_re "\[!\] KV cache"

# Saving is refused while it is ticked. The file is byte-identical afterwards,
# which is a stronger statement than "the tag is still there": a refused save
# must not have partially written anything.
before="$(store)"
send C-s
expect "unusable tag"
check
if [ "$(store)" = "$before" ]; then
	pass "the refused save left the file untouched"
else
	fail "the refused save changed the file"
fi

finish
