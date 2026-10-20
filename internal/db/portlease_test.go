package db

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestReserveBackendPortPairDistinctAndReleased(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	gameA, rconA, err := store.ReserveBackendPortPair(ctx, "node-a", "server-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	gameB, _, err := store.ReserveBackendPortPair(ctx, "node-a", "server-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if gameA == gameB || gameA == rconA || gameB == rconA {
		t.Fatalf("ports collided: a=%d/%d b=%d", gameA, rconA, gameB)
	}
	if rconA != gameA+10 {
		t.Fatalf("rcon = %d, want %d", rconA, gameA+10)
	}

	other, _, err := store.ReserveBackendPortPair(ctx, "node-b", "server-c", nil)
	if err != nil {
		t.Fatal(err)
	}
	if other != gameA {
		t.Fatalf("other node got %d, want the same first port %d", other, gameA)
	}

	if err := store.ReleaseBackendPorts(ctx, "server-a"); err != nil {
		t.Fatal(err)
	}
	again, _, err := store.ReserveBackendPortPair(ctx, "node-a", "server-d", nil)
	if err != nil {
		t.Fatal(err)
	}
	if again != gameA {
		t.Fatalf("released port %d was not reused, got %d", gameA, again)
	}
}

func TestReserveBackendPortPairSkipsDockerAndServerPorts(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	if err := store.CreateServer(ctx, &Server{
		ID: "held", Name: "held", ModLoader: ModLoaderVanilla, MCVersion: "1.21",
		NodeID: "node-a", Status: StatusStopped, Port: 25566, DataPath: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	game, _, err := store.ReserveBackendPortPair(ctx, "node-a", "server-new", map[int]bool{25567: true, 25577: true})
	if err != nil {
		t.Fatal(err)
	}
	if game == 25566 || game == 25567 {
		t.Fatalf("allocated a used port %d", game)
	}
}

func TestReserveBackendPortPairConcurrent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	const n = 8
	ports := make([]int, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ports[i], _, errs[i] = store.ReserveBackendPortPair(ctx, "node-a", fmt.Sprintf("server-%d", i), nil)
		}(i)
	}
	wg.Wait()
	seen := map[int]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if seen[ports[i]] {
			t.Fatalf("duplicate port %d", ports[i])
		}
		seen[ports[i]] = true
	}
}
