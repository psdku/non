package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"non/internal/meta"
)

// View must never panic, whatever the terminal size or content, and must never
// draw outside its box.
func TestViewSizes(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf", "c.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{
		Title:    strings.Repeat("Very Long Title ", 8),
		Author:   strings.Repeat("Author, ", 40),
		Tags:     []string{"one", "two", "three", "four", "five", "six"},
		Abstract: strings.Repeat("abstract words here ", 200),
		Comment:  strings.Repeat("comment ", 50),
	})
	m.lib.Store.Set("b.pdf", &meta.Entry{}) // empty entry
	m.lib.Rescan()
	m.rebuild()

	modes := []mode{modeBrowse, modeSearch, modeActions, modeRename, modeOpener, modeInspect,
		modeHelp, modeSettings, modeMetaForm}
	rng := rand.New(rand.NewSource(1))
	for _, mo := range modes {
		for _, w := range []int{1, 2, 5, 10, 20, 21, 40, 80, 200} {
			for _, h := range []int{1, 2, 5, 6, 7, 20, 60} {
				mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
				c := mm.(*Model)
				c.mode = mo
				switch mo {
				case modeSettings:
					c.openSettings()
				case modeMetaForm:
					c.openMetaForm()
				}
				c.actions = []action{{name: "Open with…", key: "o"}, {name: "A very long action name that will not fit", key: "x"}}
				c.cursor = rng.Intn(3)
				c.details = rng.Intn(400)
				out := c.View()
				if out == "" {
					t.Fatalf("empty view for %v %dx%d", mo, w, h)
				}
				lines := strings.Split(out, "\n")
				if len(lines) > h && !(w < 20 || h < 6) {
					t.Fatalf("%dx%d: view is %d lines tall", w, h, len(lines))
				}
				if w >= 20 && h >= 6 {
					for i, l := range lines {
						if got := len([]rune(stripAnsi(l))); got > w {
							t.Fatalf("%dx%d: line %d is %d cells: %q", w, h, i, got, l)
						}
					}
				}
			}
		}
	}
}

func TestViewNoPapers(t *testing.T) {
	m := newTestModel(t)
	for _, mo := range []mode{modeBrowse, modeActions, modeInspect} {
		m.mode = mo
		if out := m.View(); out == "" {
			t.Fatalf("empty view in mode %v", mo)
		}
		_ = fmt.Sprint(m.statusLine())
	}
}

// Wide (CJK, emoji) file names must not break the two-column layout.
func TestViewWideChars(t *testing.T) {
	m := newTestModel(t, "日本語の論文.pdf", "emoji🎉paper.pdf", strings.Repeat("あ", 40)+".pdf")
	for _, w := range []int{20, 21, 30, 45, 60, 80} {
		mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 12})
		c := mm.(*Model)
		for i, l := range strings.Split(c.View(), "\n") {
			if got := lipgloss.Width(l); got > w {
				t.Errorf("%dx12: line %d is %d cells wide", w, i, got)
			}
		}
	}
}

// The fuzzy ranking must put a title hit above an abstract hit.
func TestFuzzyRanking(t *testing.T) {
	m := newTestModel(t, "aaa.pdf", "bbb.pdf")
	m.lib.Store.Set("aaa.pdf", &meta.Entry{Title: "Nothing", Abstract: "we build vLLM, a serving system"})
	m.lib.Store.Set("bbb.pdf", &meta.Entry{Title: "vLLM: PagedAttention", Abstract: "x"})
	m.lib.Rescan()
	m.metaUI = true
	m.quat = "vllm"
	m.rebuild()
	if len(m.visible) < 2 {
		t.Fatalf("visible = %v", m.visibleNames())
	}
	if got := m.current().Name; got != "bbb.pdf" {
		t.Errorf("first match = %q, want bbb.pdf (title hit before abstract hit)", got)
	}
}

// A query that matches inside a long file name must still find the file; the
// fuzzy score for such a match is negative and must not be treated as "no
// match".
func TestFuzzyLongFilename(t *testing.T) {
	long := "a very long descriptive name about machine learning experiments 2024 final.pdf"
	m := newTestModel(t, long, "short.pdf")
	m.quat = "final"
	m.rebuild()
	if len(m.visible) != 1 {
		t.Fatalf("visible = %v, want the long file", m.visibleNames())
	}
	m.quat = "xyzzy"
	m.rebuild()
	if len(m.visible) != 0 {
		t.Errorf("visible = %v, want none", m.visibleNames())
	}
}
