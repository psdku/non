# Non (moon)

"논"문 관리자

A small manager for the PDF papers in a directory: browse them, keep metadata
for them, and open them with whatever program you like.

![non](https://img.shields.io/badge/binary-single%20static-informational)

## Why

A paper directory fills up with files like `1706.03762v7.pdf`. You forget what
they are, what you thought of them, and which ones you have read. `non` is glue:
it lists the PDFs in the current directory, extracts a title, authors and
abstract the first time it sees each one, lets you edit that metadata in
`$EDITOR`, and opens the file in your reader.

It deliberately is **not** a file manager or a terminal PDF reader.

## Install

```
go build -o non .          # or: go install ./
```

A single static binary; copy it to any machine with a shell.

`pdftotext` (poppler) is used when present, because it gives the best title and
abstract extraction. Without it, `non` falls back to a pure Go PDF parser and
still works.

## Usage

```
non                 # browse the PDFs in the current directory
non DIR             # browse another directory
non --opener firefox                 # add firefox as the default tool
non --tool 'zathura=zathura --fork,tui'   # add a tool (repeatable)
non --editor nvim
non --extract paper.pdf     # print the extracted metadata and exit
non --list                  # print one line per paper (for scripts)
non --query paged           # print the papers matching a text query
non --tags 'LLM && !DB'     # print the papers matching a tag expression
```

Flags may follow the directory (`non . --list` is the same as `non --list .`).

### Keys

| key | action |
| --- | --- |
| `j` `k` `↓` `↑` `ctrl+n` `ctrl+p` | move the cursor |
| `ctrl+f` / `ctrl+b` | page down / up |
| `g` / `G` | first / last |
| `f` | filter by tag expression; `enter` keeps it, `esc` clears it |
| `/` | fuzzy search; `enter` keeps the query, `esc` clears it |
| `tab` | also search inside metadata (author, tags, abstract) |
| `enter` | action menu for the selected paper |
| `o` | open with the default tool |
| `O` | open with… (pick a tool for this once) |
| `r` | rename the file (metadata follows it; a cleaned-up name asks first) |
| `m` / `ctrl+e` | edit the metadata (built-in form by default) |
| `s` | settings: editing mode, tools, tag table |
| `e` | re-extract the metadata from the PDF |
| `i` | inspect the file: size, pages, PDF info dictionary |
| `J` / `K` | scroll the info pane |
| `R` | rescan the directory |
| `?` | help |
| `q` / `ctrl+c` | quit |

## Directories

`non` opens one directory at a time. Which one is decided in this order:

1. a command line argument: `non .`, `non ~/papers`, `non ../../storage`;
2. the **default directory**, if set;
3. the current directory.

The default directory lives in `~/.config/non/config.json` (or
`$XDG_CONFIG_HOME/non/config.json`), because it has to be readable *before* a
directory is chosen and so cannot live in the per-directory `.non.json`:

```json
{ "default_dir": "/home/me/papers" }
```

| flag | effect |
| --- | --- |
| `--set-dir DIR` | set the default directory |
| `--unset-dir` | clear it |
| `--default-dir` | print it |

That file also holds optional `tools` and `tags` that a **collection inherits
when it has none of its own**. A collection that defines its own tools or tag
table keeps them, so a curated table is never overwritten; a fresh directory
picks up your defaults without any copying. The settings modal still writes to
the collection's `.non.json`.

## Metadata

Metadata lives in `.non.json` next to the PDFs. It is a plain JSON document, so
it can be diffed, versioned in git, or edited by another tool.

The document carries a `version` so a schema change can be migrated instead of
guessed at:

```json
{
  "version": 1,
  "entries": {
    "1706.03762v7.pdf": {
      "title": "Attention Is All You Need",
      "author": "Ashish Vaswani, Noam Shazeer, …",
      "tags": ["transformer", "attention"],
      "abstract": "The dominant sequence transduction models …"
    }
  },
  "config": {
    "default_tool": "zathura",
    "tools": [
      {"name": "zathura", "command": "zathura --fork", "type": "tui"},
      {"name": "firefox", "command": "firefox", "type": "gui"}
    ],
    "tags": [
      {"name": "kvcache", "description": "KV cache and paged attention"}
    ]
  }
}
```

Keys that `non` does not know — in an entry, in the config, or at the top level
— are preserved verbatim, so extra fields (`doi`, `year`, `rating`, …) survive
a round trip.

Versioning follows three rules:

* a purely additive change (a new optional key) does **not** bump the version;
  older builds keep the unknown key verbatim.
* a change that alters the shape or meaning of existing data bumps the version
  and adds a migration step in `internal/meta/version.go`, applied on load.
* a document written by a **newer** build is refused with an error rather than
  rewritten, because downgrading a schema you do not understand is how data is
  lost. The file is left untouched:

  ```
  non: .non.json: store was written by a newer version of non:
       file is version 99, this binary understands 1
  ```

`TestStoreMigrationChain` checks that the migration list is contiguous, so
adding a step without wiring it up fails the test suite rather than the user.

A file with no entry is extracted automatically the first time `non` starts in
that directory. Such an entry is marked `"auto": true` until you edit it by
hand; automatic entries are re-derivable, hand-written ones are never
overwritten.

## Scripting

The browser is for reading; scripts want a list. `--query` and `--tags` print
the selection and exit, using the same filter-then-rank path the TUI uses, so a
script sees exactly the papers and the order a human would.

```
$ non --query paged
2309.06180v1.pdf

$ non --tags 'LLM && !DB' --paths
/home/me/papers/hot-or-cold.pdf
/home/me/papers/think-twice.pdf

$ non --tags LLM --count
2

$ # open every paper tagged kvcache at once
$ non --tags kvcache --paths | xargs -d '\n' zathura
```

| flag | effect |
| --- | --- |
| `--query TEXT` | rank the collection against TEXT |
| `--paths` | print full paths instead of names |
| `--query ""` | the whole collection, in library order |
| `--tags EXPR` | keep only papers matching a tag expression |
| `--count` | print only the number of matches |
| `--search-meta` | let `--query` match author, tags and abstract too |
| `--no-write` | never modify `.non.json` (extract into a temporary entry) |
| `--check` | report tag problems and exit non-zero, writing nothing |

`--tags` composes with `--list` as well, so `non --list --tags LLM` lists the
LLM papers with their full records. Unknown tag names produce a warning on
stderr and simply select nothing — a typo must not silently mean "everything".

The output is one file name per line and nothing else, so it feeds a pipeline
without any cutting; use `--paths` for full paths or `--list` when a human wants
the metadata too. Text matching is tiered exactly as in the TUI: file name and
title first, then author and tags, then the body. An empty result is not an
error (exit 0), so `if [ "$(non --tags x --count)" -gt 0 ]` reads naturally.

## Settings

`s` opens the global settings modal: a floating box over the browser, dimming
it but keeping it visible. Everything in it is stored in the `config` object
above, so it is versioned together with the metadata.

| setting | meaning |
| --- | --- |
| **Open editor** | off (default): `m` opens the built-in form. on: `m` opens `$EDITOR` on a Markdown file. |
| **Search metadata** | default for the `tab` toggle in the search bar |
| **Default tool** | what plain `o` runs |
| **Manage tools** | the list behind `o` and `O` |
| **Manage tags** | the global tag table |

### Tools

A tool has a `name`, a `command` and a `type`:

* `gui` — started and left running; the TUI keeps the terminal.
* `tui` — takes over the terminal until it exits, then `non` repaints. This is
  what you want for `zathura`, `nvim`, `less`, `pdftotext`-style viewers.

Tools can also be added from the command line:

```
non --tool 'zathura=zathura --fork,tui' --tool 'firefox=firefox'
non --opener firefox        # shorthand: a gui tool, made the default
```

With no tools configured, `non` falls back to the platform opener
(`open`/`xdg-open`/`start`).

### Filtering by tag

`f` opens a filter prompt. The expression language is small enough to type from
memory, and symbols and words are interchangeable:

| form | meaning |
| --- | --- |
| `LLM` | tagged `LLM` |
| `!LLM` / `not LLM` | tagged anything but `LLM` |
| `LLM && DB` / `LLM and DB` / `LLM DB` | both |
| `LLM \|\| DB` / `LLM or DB` | either |
| `!LLM && (DB or survey)` | grouping, as usual |
| `"KV cache"` | a tag name containing spaces or operators |

`&&` binds tighter than `||`, and `!` tighter than both, so `LLM || DB && survey`
is `LLM || (DB && survey)`.

The filter is **live**: the list narrows on every keystroke, and an expression
that has not finished parsing (say `LLM &&`) keeps the last good result on
screen instead of emptying it. The parse error is shown at the right of the prompt line,
with the position of the offending character.

The filter and the search **compose** rather than compete: the filter narrows
the library to a set, then `/` ranks what is left. Both can be active at once;
`esc` peels them off one at a time, search first. The header shows the active
filter in brackets (`[ LLM && !DB ]`) and the status bar shows `filter 3/40`, so
a short list is never a mystery.

Names are matched case-insensitively against the global tag table. A name that
is not in the table matches nothing and is reported as `no such tag: …`, which
is the cheap way to catch a typo:

```
f> !LLm && DB
   no such tag: LLm
```

### Importing tags

Tag lists are shared, so they can be imported and exported as CSV —
`encoding/csv` is in the standard library, and a tag list is a flat
name+description table that fits a spreadsheet:

```
$ non --export tags.csv
$ non --import shared-tags.csv
/tmp/papers/.non.json: added 3, skipped 1 existing
  + LLM
  + kvcache
  + quant
```

The first row may be a header; otherwise column 1 is the name and column 2 the
description. Import **only adds tags that are not already there**, so a shared
list is a starting point and re-importing it can never undo a local rename or a
description you wrote yourself. That also makes `--import` safe to repeat.

`-g` targets the user config (`~/.config/non/config.json`) instead of the
current collection, for building up a set of tags that new directories inherit.

Names are sanitized on import (`kv-cache` → `kv_cache`), and rows that leave
nothing usable are reported rather than added.

### Tags

Tags are not free text. A name may contain letters, digits, `_`, `-` and `&` —
enough for `attention-mechanisms` and `R&D`.
Quotes, backslashes, `,`, `:`, `!`, `|` and `&&` are reserved, because a name
holding one could not be written in a filter expression without quoting. A lone
`&` is fine: it is only the pair that means AND.

A **space is deliberately not allowed**. Two names written next to each other
mean AND in a filter, so a name with a space could only be queried by quoting it
every time — a tax on every query to pay for something `-` or `_` expresses just
as well. A CSV import of `KV cache` stores `KV-cache`.

**Parentheses are not allowed either.** They look tempting for a qualifier —
`retrieval-(dense)` — but the tag description says the same thing better, and a
parenthesis is a grouping operator in the filter grammar, so every use of such a
name would need quoting. Sanitizing turns them into dashes: `retrieval (dense)`
becomes `retrieval-dense`.

The rule is enforced when a name is written, never when the file is read — an
older store with a name outside it still opens, and the name is reported (see
below) rather than making the collection unusable.

The global table gives every tag a **name** — what
shows on the paper and in the list — and a **description** — what makes the
table worth reading. The built-in metadata form shows the table as a picker, so
a paper can only carry tags that exist; a new tag is created from the "add tag"
row and immediately joins the table. Renaming or deleting a tag in the table
follows through to every paper that uses it.

Tag names are quoted wherever they are joined on one line, so `a b` as a single
tag cannot be confused with the two tags `a` and `b`. The picker is a scrolling
window, not a wall of chips: a collection with hundreds
of tags would otherwise push every other field off the screen. It shows how many
tags there are, how many are hidden, and how often each is used; `/` filters it
(the query may hold any character, including a space) and the arrow keys
move. The form's height is the same whether the table has thirty tags or three
hundred.

A tag name is unique **regardless of case**: `llm`, `LlM` and `LLM` are one tag,
and adding or renaming onto an existing spelling never creates a second row
nobody could tell apart. Adding a tag that already exists selects it and says so,
because the intent — "this paper carries this tag" — is fulfilled either way.

#### When the file is wrong

The metadata file can be edited by hand, or by another script. `non` treats
problems by how far they get in the way, because only one kind can be fixed
without losing information:

* **Duplicates are repaired on load, silently.** A tag repeated in one entry, or
  twice in the table with different case, is merged. Nothing is lost — the tag
  survives, and a description from the dropped row is kept — so there is nothing
  to ask about. The repair is in memory: the file is only rewritten when
  something else saves it, so a read never changes what is on disk.
* **An unknown tag is reported, never deleted.** A tag on a paper that the table
  does not know is *hidden from the display* — the list row, the details pane,
  the footer and the `--list` tags column — because showing it as if it were real
  makes the warning easy to forget. It stays in the file, and the metadata form
  lists it in red as `[!] typo` and refuses to save while it is ticked: either
  untick it (removing it), or add it in the tag table. A typo is one keystroke
  from a real tag, and unrecoverable once deleted, so the decision stays with you.

  Queries still match it. Hiding a tag from the display says "fix this"; hiding it
  from the query language too would leave the paper unreachable until the file was
  repaired, and a quoted name is an explicit act. When a filter *did* match on
  such a tag, the row shows it in red — otherwise the user would be looking at a
  file that appears to match nothing.
* **A name that would break a query is treated the same way** — hidden from the
  display, marked `[!]` in the picker, blocking on save — because `KV cache` or
  `a|b` could only be queried by quoting. Quoting is supported for exactly this
  case, so a query still works before the file is fixed:

  ```
  non --tags '"KV cache"' .
  ```
* **A known name with an untidy character is only reported.** `weird.name` or
  `a/b` lexes as a bare name, so it displays, filters and saves normally;
  `--check` mentions it and exits 0. The write rule keeps new data tidy; enforcing
  it on old data would hide the user's own tags over a punctuation mark.

The warning appears in the status bar on startup, and on stderr for the
non-interactive flags:

```
$ non --list
non: warning: 1 unusable tag(s): typo
     fix them with the metadata editor (m), or add them in settings (s)
```

`--check` reports problems, without writing anything, for a script that
generates metadata. It exits non-zero only for what actually blocks — an unknown
tag, or a name that cannot be queried — and exits 0 when everything found is
merely untidy:

```
$ non --check
.non.json: 1 problem(s)
  a.pdf: tag "typo" is not in the tag table
```

### File names

A file name is shown on one line, used as a metadata key, and matched by search,
so `non` normalizes a rename instead of accepting something that would break
those:

| in a typed name | becomes |
| --- | --- |
| quotes | removed |
| newline, tab | a single space |
| control characters | removed |
| path separators | the path is reduced to its last component |
| runs of whitespace | a single `-` |

A rename that would change the name opens a confirmation dialog first, saying
what was removed and what the file will be called, so nothing is rewritten
silently.

### The `$EDITOR` format

`m` opens a small Markdown document:

```markdown
# Attention Is All You Need
- Author: Ashish Vaswani, Noam Shazeer
- Tags: transformer, attention

# Comment
Re-read section 3.2 before the meeting.

# Abstract (autogenerated)
The dominant sequence transduction models are based on complex recurrent …
```

The first `#` heading is the title and is mandatory — `non` refuses to save a
document without one. A document that does not parse, or that names a tag the
tag table does not know, is **discarded**: the metadata is left unchanged and the
error says so. The editor's text is *not* deleted with it — the temp file is kept
and its path goes into the message, so one wrong tag does not cost the whole
edit. `- Author:` and `- Tags:` are recognised bullets; any
other `- Key: value` bullet becomes an extra field. `# Comment` and
`# Abstract` hold free text (a suffix in parentheses is ignored). Any other
heading is stored as an extra field whose value is the section body, so new
fields need no code change.

## Look

The interface follows [yazi](https://yazi-rs.github.io)'s default manager
layout, without nerd icons or powerline glyphs:

* two bordered panes — list and details — with the focused one's border in blue
  and the other in gray, so focus never shifts the layout;
* a list row is the **file name** with its **tags flush right**, and nothing
  else: the title, author and abstract already have the details pane, and
  repeating them on every row turned the list into two competing columns. More
  than three tags are summarized as `alpha beta gamma+`; the details pane always
  shows the full list;
* the current line is a single reverse-video bar, not a coloured arrow;
* panel titles are written into the top border (`3 papers`, `details`);
* menus are small floating boxes drawn over the list;
* a status bar along the bottom: a reverse-video mode block (`NORMAL`,
  `SEARCH`, `FILTER`, `ACTION`, `OPEN`, `RENAME`, `INSPECT`), the paper count or
  the last message, the active tag filter, then the key hints on the right;
* accents are used sparingly — cyan for the directory and keys, yellow for
  tags, magenta for the selected label, red for errors;
* modals (settings, the metadata form, help) are centred floating boxes over a
  dimmed browser, not separate screens;
* a modal's bottom three rows are fixed — a separator, the **description of
  whatever is under the cursor**, then the keymap. The description used to sit
  one row below the setting it explained, which made the whole box shift by a
  row every time the cursor moved.

Colours are ANSI 256 colour indices, so the look tracks the terminal's palette
rather than hard-coding RGB values.

## Scaling

Metadata is one JSON file that is read at start-up and rewritten in full on
every edit. That is deliberately simple, but it is linear work per keystroke,
so the question is where it stops being reasonable.

Measured with `go test ./internal/meta -run TestStoreScaleCurve -v`, using the
actual `Store` and `Entry` types and a realistic entry (the Transformer
abstract, ~1.2 kB, eight authors, two tags, a comment):

| papers | file size | load | save |
| --- | --- | --- | --- |
| 100 | 0.14 MB | 0.6 ms | 1.1 ms |
| 1 000 | 1.4 MB | 7 ms | 9 ms |
| 5 000 | 6.9 MB | 32 ms | 36 ms |
| 10 000 | 13.7 MB | 65 ms | 75 ms |
| 20 000 | 27.5 MB | 129 ms | 152 ms |

The cost is linear, ≈6.5 µs per paper to load and ≈7.5 µs to save. For a
human, the interesting thresholds are roughly:

* **≤ 2 000 papers** — indistinguishable from instant. Load and save are both
  under 15 ms, well below a frame of the TUI.
* **≤ 10 000 papers** — still fine. A 65 ms start-up and a 75 ms save are
  noticeable only as a slightly late first paint; nothing is blocking a
  keystroke, because the save happens after the edit is applied.
* **≥ 20 000 papers** — the save starts to be felt (150 ms of the whole
  process being unresponsive per edit) and the file is big enough that a
  partial write or a merge conflict in git becomes a real annoyance.

So the JSON store is the right choice up to about ten thousand papers, which is
far more than a personal paper collection. Two things would change the answer:

1. **Concurrency.** Two `non` processes on the same directory already race; the
   last writer wins. A database with real transactions would fix that, JSON
   cannot.
2. **Querying without loading.** An index that lets you search 100 000 papers
   without parsing all of them needs SQLite (or a sidecar index).

Neither applies today. If either starts to, the migration is contained: `Store`
is the only file format, `Load`/`Save` are the only I/O paths, and the tests
already pin the in-memory API (`Set`, `Lookup`, `Delete`, `Rename`,
`TagsInUse`, …). Swapping the backing store for `modernc.org/sqlite` would not
touch the TUI.

## Extraction

Extraction runs the first time a paper is seen and on demand with `e`:

1. **Title** — the topmost vertically contiguous block of lines set in the
   largest type on page one, skipping boilerplate (arXiv stamps, copyright
   notices) and rotated text (axis labels). The PDF's Info-dictionary title is
   used when the layout gives nothing, or when it is a close variant (it is the
   cleaner copy when the fallback parser mangles ligatures).
2. **Authors** — the name lines between the title and the abstract, skipping
   affiliations, emails and superscript markers.
3. **Abstract** — the text following an `Abstract` heading in the same column,
   stopping at `1 Introduction`, keywords, or the next section.

## Layout

```
main.go                  flags, wiring, --extract / --list / --query
internal/meta            the .non.json store (entries, tools, tags, the global
                         config, tag CSV) and the Markdown editor format
internal/extract         title / author / abstract extraction
internal/app             directory scanning, rename, inspect, library logic and
                         the Query that both the TUI and --query use
internal/filter          the tag filter expression parser and evaluator
internal/search          the tiered text ranking, shared by both front ends
internal/tui             the Bubble Tea model, update and view, plus the
                         settings modal and the built-in metadata form
```

## Tests

```
go test ./...      # the model
tests/run.sh       # the terminal: the real binary in tmux, driven by keys
```

The Go tests cover the model, the parser, the store and the rendering functions.
The `tests/` scripts cover what only the terminal can show: a key that never
reaches its handler, a view that does not repaint, a cursor drawn twice. They
need `tmux` and run in about four seconds; see `tests/README.md`.

The sample PDFs live in `samples/` and are git-ignored: they are fixtures, not
content.

The extraction tests use the three papers in `samples/` and assert their real
titles, authors and abstracts; they are skipped when the PDFs are absent, so a
fresh clone still tests clean.
