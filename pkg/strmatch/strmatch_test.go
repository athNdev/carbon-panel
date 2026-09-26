// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package strmatch

import (
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
