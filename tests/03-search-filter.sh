#!/usr/bin/env bash
# Search and filter: enter keeps the query, esc clears it, and the two compose.
#
# The asymmetry between these two boxes was once a bug; this test pins the
# intended behaviour so it cannot flip back.

. "$(dirname "$0")/lib.sh"

start alpha.pdf beta.pdf gamma.pdf

# Search narrows the list. While typing, the prompt is the header.
send /
type_text "beta"
expect "//beta"
# Enter keeps the query: the list stays narrowed and the header shows it.
send Enter
expect "/beta"
expect "1/3"
# Esc clears it: everything is back.
send Escape
expect "3 papers"
expect_not "/beta"

# A filter is a separate box, shown bracketed. Esc clears the filter.
send f
type_text "LLM"
send Enter
expect "[ LLM ]"
# "0/3" — the pane is titled with a fraction once a filter is active, because
# "0 papers" would not say how many were hidden.
expect "0/3"
send Escape
expect "3 papers"
expect_not "[ LLM ]"

# The two compose: a filter narrows, then a search ranks within it.
send f
type_text "LLM"
send Enter
send /
type_text "alpha"
send Enter
# No paper has the tag, so the filter still wins: the search cannot widen it.
expect "0/3"
expect "[ LLM ]"
expect "/alpha"
# Esc clears the narrower thing first (the search), then the filter.
send Escape
expect_not "/alpha"
expect "[ LLM ]"
send Escape
expect "3 papers"
expect_not "[ LLM ]"

finish
