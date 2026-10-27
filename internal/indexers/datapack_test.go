// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package indexers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/config"
)

type stubDatapackIndexer struct {
	name string
	file *DatapackFile
	err  error
}

func (s *stubDatapackIndexer) SearchDatapacks(context.Context, string, string, int, int) (*DatapackSearchResult, error) {
	return &DatapackSearchResult{}, nil
}

func (s *stubDatapackIndexer) GetDatapackLatestFile(context.Context, string, string) (*DatapackFile, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.file, nil
}

func (s *stubDatapackIndexer) GetIndexerName() string { return s.name }

func TestFirstDatapackLatestFile(t *testing.T) {
	ok := &stubDatapackIndexer{name: "ok", file: &DatapackFile{Filename: "pack.zip"}}
	miss := &stubDatapackIndexer{name: "miss", err: errors.New("not found")}

	f, resolved, err := FirstDatapackLatestFile(context.Background(), "id", "", []DatapackIndexer{miss, ok})
	if err != nil || resolved != ok || f.Filename != "pack.zip" {
		t.Fatalf("file=%v resolved=%v err=%v; want first success", f, resolved, err)
	}

	if _, _, err := FirstDatapackLatestFile(context.Background(), "id", "", []DatapackIndexer{miss}); err == nil {
		t.Fatal("want joined error when no provider resolves")
	} else if !strings.Contains(err.Error(), "miss") {
		t.Fatalf("error should name the failing provider: %v", err)
	}
}

func TestDatapackIndexersFor(t *testing.T) {
	stub := &stubDatapackIndexer{name: "stub-provider"}
	RegisterDatapackIndexer("stub-provider", func(string, *config.Config) DatapackIndexer { return stub })

	// Named resolution returns exactly that provider.
	idxs, err := DatapackIndexersFor("stub-provider", "", &config.Config{})
	if err != nil || len(idxs) != 1 || idxs[0] != stub {
		t.Fatalf("named: idxs=%v err=%v", idxs, err)
	}

	// Unknown names fail loudly.
	if _, err := DatapackIndexersFor("nope", "", &config.Config{}); err == nil {
		t.Fatal("want error for unknown indexer")
	}

	// Empty name fans out to every registered provider, aliases normalized
	// ("fuego" resolves to the same registry entry as "curseforge").
	if got := normalizeDatapackIndexerName("fuego"); got != "curseforge" {
		t.Fatalf("alias normalization = %q", got)
	}
	idxs, err = DatapackIndexersFor("", "", &config.Config{})
	if err != nil || len(idxs) < 1 {
		t.Fatalf("fan-out: idxs=%v err=%v", idxs, err)
	}
}
