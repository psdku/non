package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tests here answer one question: how far does the single JSON file scale
// before it needs to become a database? The numbers are summarised in the
// README under "Scaling".
//
// The model of paper metadata used below is a real one: the abstract of the
// Transformer paper (~1.2 kB), eight authors, two tags and a comment. That is
// what non actually writes for the papers in this directory, so the sizes here
// are not optimistic.

const scaleAbstract = "The dominant sequence transduction models are based on complex recurrent or " +
	"convolutional neural networks that include an encoder and a decoder. The best performing " +
	"models also connect the encoder and decoder through an attention mechanism. We propose a new " +
	"simple network architecture, the Transformer, based solely on attention mechanisms, dispensing " +
	"with recurrence and convolutions entirely. Experiments on two machine translation tasks show " +
	"these models to be superior in quality while being more parallelizable and requiring " +
	"significantly less time to train. Our model achieves 28.4 BLEU on the WMT 2014 " +
	"English-to-German translation task, improving over the existing best results, including " +
	"ensembles, by over 2 BLEU. On the WMT 2014 English-to-French translation task, our model " +
	"establishes a new single-model state-of-the-art BLEU score of 41.8 after training for 3.5 days " +
	"on eight GPUs, a small fraction of the training costs of the best models from the literature."

// fillStore builds a store with n realistic entries.
func fillStore(n int) *Store {
	s := New()
	for i := range n {
		s.Set(fmt.Sprintf("1706.%05dv7.pdf", i), &Entry{
			Title:    "Efficient Memory Management for Large Language Model Serving",
			Author:   strings.Repeat("Author Name, ", 8),
			Abstract: scaleAbstract,
			Tags:     []string{"kvcache", "serving"},
			Comment:  "Re-read section 3.2 before the meeting.",
			Auto:     true,
		})
	}
	return s
}

// storeCost measures a full load and a full save, which is what every edit
// costs: the file is rewritten as a whole.
func storeCost(t *testing.T, n int) (size int64, load, save time.Duration) {
	t.Helper()
	dir := t.TempDir()
	s := fillStore(n)
	if err := s.SaveTo(dir); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, StoreFile))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	back, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	load = time.Since(start)
	if len(back.Entries) != n {
		t.Fatalf("loaded %d entries, want %d", len(back.Entries), n)
	}
	start = time.Now()
	if err := back.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	save = time.Since(start)
	return info.Size(), load, save
}

// TestStoreScaleCurve records the cost curve. It is not a correctness test, it
// is the measurement the storage decision rests on, so it prints its numbers
// under -v.
func TestStoreScaleCurve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the scale measurement in -short mode")
	}
	for _, n := range []int{100, 1000, 5000, 10000, 20000} {
		size, load, save := storeCost(t, n)
		t.Logf("%6d papers  %6.2f MB  load %7.2f ms  save %7.2f ms",
			n, float64(size)/1e6, ms(load), ms(save))
	}
}

// TestStoreScalesToTenThousandPapers is the guard: at the size where a JSON
// store would start to feel slow, a full load still fits inside one keystroke
// and a save does not stall the UI.
func TestStoreScalesToTenThousandPapers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the scale test in -short mode")
	}
	const n = 10000
	size, load, save := storeCost(t, n)
	t.Logf("%d papers, %.1f MB: load %v, save %v", n, float64(size)/1e6, load, save)
	if raceEnabled {
		// The race detector slows the JSON codec down by more than an order of
		// magnitude, so the numbers below say nothing about real use.
		t.Skip("timing thresholds are meaningless with -race")
	}
	if load > 250*time.Millisecond {
		t.Errorf("loading %d papers took %v; browsing would feel slow", n, load)
	}
	if save > 250*time.Millisecond {
		t.Errorf("saving %d papers took %v; an edit would stall the UI", n, save)
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
