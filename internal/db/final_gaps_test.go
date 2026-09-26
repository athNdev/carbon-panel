package db

import (
	"context"
	"testing"
)

func TestCreateSnapshot_NilRejected(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	if err := store.CreateSnapshot(context.Background(), nil); err == nil {
		t.Error("nil snapshot accepted")
	}
}

func TestSeedDefaultNode_NoOp(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	if err := store.SeedDefaultNode(); err != nil {
		t.Errorf("no-op seed failed: %v", err)
	}
}
