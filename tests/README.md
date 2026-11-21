# TUI tests

Go tests cover the model. These cover the *terminal*: the real binary, running
in tmux, driven with keystrokes, asserted against what the screen actually
shows.

They exist because a whole class of bug is invisible to unit tests — a key that
never reaches a handler, a view that does not repaint, a message that is drawn
and never cleared, a cursor drawn twice. Every one of those was a real bug in
this program, found by a human looking at the screen.

## Running

```sh
tests/run.sh          # every test
tests/run.sh 04       # the ones whose name contains "04"
tests/04-tags.sh      # one test, directly
```

Exit status is 0 only if everything passed. Requires `tmux`.

## How a test works

```bash
. "$(dirname "$0")/lib.sh"

start alpha.pdf beta.pdf      # build, make a scratch collection, open the TUI

send m                        # a key, by NAME: "m", "Enter", "Tab", "C-s"
type_text "KV cache"          # literal text, so a space or a quote arrives
expect "edit metadata"        # the screen contains this
expect_not "loading"          # and not this
expect_re "\[x\] LLM"         # a regex, when the exact text varies

store_has '"author": "X"'     # the metadata file contains this

finish                        # kill the session, print a tally, set the status
```

Assertions are counted, so a failing check does not stop the test: one run shows
every problem instead of only the first.

Useful environment variables:

| variable | effect |
|---|---|
| `KEEP_SCREEN=1` | print the screen after each failure |
| `KEEP_WORK=1` | keep the scratch directory |
| `NO_COLOR=1` | plain output |
| `NON_TEST_COLS` / `NON_TEST_ROWS` | terminal size (default 100×28) |
| `NON_TEST_SOCKET` | tmux socket (default `nondev`, **never the default one**) |

A test that needs a second collection calls `start` again; each call makes its own
scratch directory, and all of them are removed at exit.

## Rules that keep these honest

* **Never the user's data.** Each test runs in a fresh `mktemp` directory, is
  given `.` as its argument, and gets `XDG_CONFIG_HOME` inside that directory.
  Without the explicit `.`, `non` opens the configured default directory, which
  is the user's real collection.
* **Never the user's tmux.** The socket is `-L nondev`. Killing the default
  server once killed an attached session of the person running the tests.
* **Press, do not count.** `press_until k "› Manage tags"` is stable when a row
  is added; "press up four times" is not.
* **Assert on files, not only screens.** A screen that says "saved" while the
  file says otherwise is exactly the bug worth catching, so the file is checked
  too — and a refused save is checked to leave the file byte-identical.
* **One behaviour per test file**, named for it.

## What is covered

| file | behaviour |
|---|---|
| `01-startup.sh` | panes, count, status bar, help, quitting |
| `02-metadata.sh` | the form opens, one cursor, typing, saving to disk |
| `03-search-filter.sh` | search and filter boxes, enter keeps, esc clears |
| `04-tags.sh` | picker scrolls and filters, hidden tags marked, duplicates warn |
| `05-settings.sh` | modal floats, messages clear on a key, edits reach the file |
| `06-actions.sh` | action menu, rename confirmation, yes and no |

This is a proof of concept: six tests, one behaviour each. If it proves useful,
the next things worth covering are opening a tool that takes over the terminal
(the `tui` tool path), extracting metadata from a real PDF, and the inspect
panel.

## Proving the harness itself

A test suite that always passes is worthless. To check that these can fail, break
something and watch. Verifying this suite, the clear at the top of
`handleSettingsKey` (the fix for "the saved message never goes away") was
commented out:

```
FAIL screen should not show: saved
FAIL screen does not show: space toggle
2 of 18 checks failed
```

Two assertions caught it, from two directions: the message was still there, and
the keymap had not come back.

The check is that a *specific* test fails for a *specific* reason. If a change
makes everything fail, the harness is broken; if nothing fails, the assertion is
missing.
