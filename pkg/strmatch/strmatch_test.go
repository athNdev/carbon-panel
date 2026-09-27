package strmatch

import (
	"math"
	"testing"
)

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
