// Merged in the MINE-172 integration: this file existed independently on
// both lines of development, so the union of both test sets is kept.

package strmatch

import (
	"math"
	"testing"
)

func TestBest_ExactAndFuzzy(t *testing.T) {
	candidates := []string{"forge", "fabric", "neoforge", "quilt", "paper"}

	m, ok := Best("forge", candidates)
	if !ok || m.Value != "forge" {
		t.Errorf("exact = %+v, %v", m, ok)
	}

	m, ok = Best("fabrc", candidates)
	if !ok || m.Value != "fabric" {
		t.Errorf("fuzzy = %+v, %v", m, ok)
	}

	// Best is best-effort: found reports whether candidates was non-empty.
	// Score gating is BestAbove's job.
	if _, ok := Best("zzzz-no-match", candidates); !ok {
		t.Error("non-empty candidates should report found")
	}
	if _, ok := Best("forge", nil); ok {
		t.Error("nil candidates matched")
	}
}

func TestBestAbove_Threshold(t *testing.T) {
	candidates := []string{"forge", "fabric"}

	if _, ok := BestAbove("forge", candidates, 0.99); !ok {
		t.Error("exact below high threshold")
	}
	if _, ok := BestAbove("zzzz-no-match", candidates, 0.5); ok {
		t.Error("garbage above threshold")
	}
	if _, ok := BestAbove("forge", candidates, 1.01); ok {
		t.Error("threshold >1 matched")
	}
}

func TestBestFunc_GenericKey(t *testing.T) {
	type item struct {
		name string
		id   int
	}
	items := []item{{"forge", 1}, {"fabric", 2}}

	best, score, ok := BestFunc("fabrc", items, func(i item) string { return i.name })
	if !ok || best.id != 2 || score <= 0 {
		t.Errorf("generic = %+v %v %v", best, score, ok)
	}
	if _, _, ok := BestFunc("x", nil, func(i item) string { return i.name }); ok {
		t.Error("nil items matched")
	}
}

func TestScore_Ordering(t *testing.T) {
	exact := Score("forge", "forge")
	close := Score("forge", "foreg")
	far := Score("forge", "paper")
	if !(exact >= close && close > far) {
		t.Errorf("scores not ordered: exact=%v close=%v far=%v", exact, close, far)
	}
	if s := Score("", ""); s < 0 {
		t.Errorf("empty score = %v", s)
	}
}

// --- tests present only on the pre-merge line of development ---

func TestScoreExactAndEmpty(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		candidate string
		want      float64
	}{
		{"both empty", "", "", 0},
		{"empty query", "", "paper", 0},
		{"empty candidate", "paper", "", 0},
		{"exact", "paper", "paper", 1.0},
		{"case insensitive", "Paper", "paper", 1.0},
		{"trimmed", "  paper  ", "paper", 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Score(tc.query, tc.candidate); got != tc.want {
				t.Fatalf("Score(%q,%q)=%v want %v", tc.query, tc.candidate, got, tc.want)
			}
		})
	}
}

func TestScoreBands(t *testing.T) {
	// Whole-token containment: high but strictly below exact.
	if s := Score("neoforge", "neoforge-21.1.228"); s <= 0.5 || s >= 1.0 {
		t.Fatalf("containment score=%v want in (0.5,1.0)", s)
	}
	// Mid-word fragment is penalized below a clean token hit.
	buried := Score("ore", "neoforge-21.1.228")
	clean := Score("neoforge", "neoforge-21.1.228")
	if !(buried < clean) {
		t.Fatalf("buried=%v clean=%v want buried<clean", buried, clean)
	}
	// Typo tolerance via edit band.
	if s := Score("papr", "paper"); s <= 0.3 || s >= 0.6 {
		t.Fatalf("typo score=%v want in (0.3,0.6)", s)
	}
	// Dissimilar strings score ~0.
	if s := Score("zzzqqqx", "paper"); s >= 0.3 {
		t.Fatalf("dissimilar score=%v want <0.3", s)
	}
	// Scores always within [0,1].
	for _, q := range []string{"a", "paper server", "vanilla 1.21", "!!!"} {
		for _, c := range []string{"b", "purpur 1.21", "velocity proxy", "???"} {
			s := Score(q, c)
			if math.IsNaN(s) || s < 0 || s > 1 {
				t.Fatalf("Score(%q,%q)=%v out of range", q, c, s)
			}
		}
	}
}

func TestBest(t *testing.T) {
	cands := []string{"vanilla", "paper 1.21", "purpur 1.21"}
	m, ok := Best("purpur", cands)
	if !ok {
		t.Fatal("Best returned not-found")
	}
	if m.Index != 2 || m.Value != "purpur 1.21" {
		t.Fatalf("Best=%+v want purpur entry", m)
	}
	if _, ok := Best("x", nil); ok {
		t.Fatal("Best on empty candidates must return found=false")
	}
	// Ties keep the first candidate.
	m, _ = Best("paper", []string{"paper", "paper"})
	if m.Index != 0 {
		t.Fatalf("tie Best index=%d want 0", m.Index)
	}
}

func TestBestAbove(t *testing.T) {
	cands := []string{"vanilla", "paper 1.21"}
	if _, ok := BestAbove("zzzqqqx", cands, 0.5); ok {
		t.Fatal("BestAbove should reject dissimilar query at 0.5")
	}
	m, ok := BestAbove("paper", cands, 0.5)
	if !ok || m.Value != "paper 1.21" {
		t.Fatalf("BestAbove=%+v ok=%v want paper entry", m, ok)
	}
	if _, ok := BestAbove("paper", nil, 0.0); ok {
		t.Fatal("BestAbove on empty candidates must return found=false")
	}
}

func TestBestFunc(t *testing.T) {
	type item struct {
		name string
	}
	items := []item{{"vanilla"}, {"paper 1.21"}, {"purpur 1.21"}}
	best, score, ok := BestFunc("purpur", items, func(i item) string { return i.name })
	if !ok || best.name != "purpur 1.21" {
		t.Fatalf("BestFunc=%+v score=%v ok=%v", best, score, ok)
	}
	if _, _, ok := BestFunc("x", nil, func(i item) string { return i.name }); ok {
		t.Fatal("BestFunc on empty items must return ok=false")
	}
}
