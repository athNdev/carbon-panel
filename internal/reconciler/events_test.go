// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package reconciler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	dockerevents "github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

// --- translateEvent: pure translation logic ---

func TestTranslateEvent_ServerContainerStart(t *testing.T) {
	msg := dockerevents.Message{
		Type:   dockerevents.ContainerEventType,
		Action: dockerevents.ActionStart,
		Actor: dockerevents.Actor{
			ID: "abc123",
			Attributes: map[string]string{
				labelServerID: "srv-1",
				labelManaged:  "true",
				"image":       "itzg/minecraft-server",
			},
		},
		Time: 1700000000,
	}

	ev, ok := translateEvent("node-a", msg)
	if !ok {
		t.Fatalf("expected event to be forwarded")
	}
	if ev.NodeID != "node-a" {
		t.Errorf("NodeID = %q, want node-a", ev.NodeID)
	}
	if ev.ServerID != "srv-1" {
		t.Errorf("ServerID = %q, want srv-1", ev.ServerID)
	}
	if ev.ModuleID != "" {
		t.Errorf("ModuleID = %q, want empty", ev.ModuleID)
	}
	if ev.ContainerID != "abc123" {
		t.Errorf("ContainerID = %q, want abc123", ev.ContainerID)
	}
	if ev.Action != "start" {
		t.Errorf("Action = %q, want start", ev.Action)
	}
	if ev.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil for start action", ev.ExitCode)
	}
	if !ev.Timestamp.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Errorf("Timestamp = %v, want %v", ev.Timestamp, time.Unix(1700000000, 0).UTC())
	}
	if ev.Attributes["image"] != "itzg/minecraft-server" {
		t.Errorf("Attributes not preserved: %v", ev.Attributes)
	}
}

func TestTranslateEvent_ModuleContainer(t *testing.T) {
	msg := dockerevents.Message{
		Type:   dockerevents.ContainerEventType,
		Action: dockerevents.ActionCreate,
		Actor: dockerevents.Actor{
			ID: "mod-container",
			Attributes: map[string]string{
				labelModuleID: "mod-42",
			},
		},
	}

	ev, ok := translateEvent("node-a", msg)
	if !ok {
		t.Fatalf("expected event to be forwarded")
	}
	if ev.ModuleID != "mod-42" {
		t.Errorf("ModuleID = %q, want mod-42", ev.ModuleID)
	}
	if ev.ServerID != "" {
		t.Errorf("ServerID = %q, want empty", ev.ServerID)
	}
}

func TestTranslateEvent_DieParsesExitCode(t *testing.T) {
	msg := dockerevents.Message{
		Type:   dockerevents.ContainerEventType,
		Action: dockerevents.ActionDie,
		Actor: dockerevents.Actor{
			ID: "c1",
			Attributes: map[string]string{
				labelServerID: "srv-1",
				"exitCode":    "137",
			},
		},
	}

	ev, ok := translateEvent("node-a", msg)
	if !ok {
		t.Fatalf("expected event to be forwarded")
	}
	if ev.ExitCode == nil {
		t.Fatalf("ExitCode = nil, want 137")
	}
	if *ev.ExitCode != 137 {
		t.Errorf("ExitCode = %d, want 137", *ev.ExitCode)
	}
}

func TestTranslateEvent_DieMissingOrBadExitCode(t *testing.T) {
	cases := []map[string]string{
		{},
		{"exitCode": "not-a-number"},
	}
	for _, attrs := range cases {
		msg := dockerevents.Message{
			Type:   dockerevents.ContainerEventType,
			Action: dockerevents.ActionDie,
			Actor:  dockerevents.Actor{ID: "c1", Attributes: attrs},
		}
		ev, ok := translateEvent("node-a", msg)
		if !ok {
			t.Fatalf("expected event to be forwarded")
		}
		if ev.ExitCode != nil {
			t.Errorf("ExitCode = %v, want nil for attrs %v", *ev.ExitCode, attrs)
		}
	}
}

func TestTranslateEvent_NonContainerTypeIgnored(t *testing.T) {
	msg := dockerevents.Message{
		Type:   dockerevents.NetworkEventType,
		Action: dockerevents.ActionConnect,
		Actor:  dockerevents.Actor{ID: "net1"},
	}
	_, ok := translateEvent("node-a", msg)
	if ok {
		t.Fatalf("expected network event to be dropped")
	}
}

func TestTranslateEvent_UnforwardedActionIgnored(t *testing.T) {
	unforwarded := []dockerevents.Action{
		dockerevents.ActionExecCreate,
		dockerevents.ActionTop,
		dockerevents.ActionArchivePath,
		dockerevents.ActionRestart,
		dockerevents.ActionKill,
	}
	for _, action := range unforwarded {
		msg := dockerevents.Message{
			Type:   dockerevents.ContainerEventType,
			Action: action,
			Actor:  dockerevents.Actor{ID: "c1"},
		}
		if _, ok := translateEvent("node-a", msg); ok {
			t.Errorf("action %q should not be forwarded", action)
		}
	}
}

func TestTranslateEvent_AllForwardedActionsPass(t *testing.T) {
	for action := range forwardedActions {
		msg := dockerevents.Message{
			Type:   dockerevents.ContainerEventType,
			Action: action,
			Actor:  dockerevents.Actor{ID: "c1"},
		}
		if _, ok := translateEvent("node-a", msg); !ok {
			t.Errorf("action %q should be forwarded", action)
		}
	}
}

func TestTranslateEvent_PrefersTimeNanoOverTime(t *testing.T) {
	msg := dockerevents.Message{
		Type:     dockerevents.ContainerEventType,
		Action:   dockerevents.ActionStart,
		Actor:    dockerevents.Actor{ID: "c1"},
		Time:     1700000000,
		TimeNano: 1700000000123456789,
	}
	ev, ok := translateEvent("node-a", msg)
	if !ok {
		t.Fatalf("expected event to be forwarded")
	}
	want := time.Unix(0, 1700000000123456789).UTC()
	if !ev.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v", ev.Timestamp, want)
	}
}

// --- computeBackoff: pure backoff calculation ---

func TestComputeBackoff(t *testing.T) {
	cases := []struct {
		retries int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 30 * time.Second}, // 32s capped to 30s
		{6, 30 * time.Second},
		{100, 30 * time.Second}, // large retry counts stay capped, no overflow
	}
	for _, c := range cases {
		got := computeBackoff(c.retries)
		if got != c.want {
			t.Errorf("computeBackoff(%d) = %v, want %v", c.retries, got, c.want)
		}
	}
}

func TestComputeBackoff_NeverExceedsMax(t *testing.T) {
	for retries := 0; retries <= 1000; retries++ {
		got := computeBackoff(retries)
		if got > backoffMax {
			t.Fatalf("computeBackoff(%d) = %v, exceeds cap %v", retries, got, backoffMax)
		}
		if got <= 0 {
			t.Fatalf("computeBackoff(%d) = %v, must be positive", retries, got)
		}
	}
}

// --- Watcher.stream: fake eventsSource, no live Docker connection ---

type fakeEventsSource struct {
	msgCh chan dockerevents.Message
	errCh chan error
}

func newFakeEventsSource() *fakeEventsSource {
	return &fakeEventsSource{
		msgCh: make(chan dockerevents.Message, 8),
		errCh: make(chan error, 1),
	}
}

func (f *fakeEventsSource) Events(_ context.Context, _ client.EventsListOptions) client.EventsResult {
	return client.EventsResult{Messages: f.msgCh, Err: f.errCh}
}

func TestWatcherStream_ForwardsTranslatedEvents(t *testing.T) {
	fake := newFakeEventsSource()
	w := &Watcher{
		nodeID: "node-a",
		client: fake,
		out:    make(chan ContainerEvent, 8),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamDone := make(chan error, 1)
	go func() { streamDone <- w.stream(ctx) }()

	fake.msgCh <- dockerevents.Message{
		Type:   dockerevents.ContainerEventType,
		Action: dockerevents.ActionStart,
		Actor:  dockerevents.Actor{ID: "c1", Attributes: map[string]string{labelServerID: "srv-1"}},
	}
	// Unforwarded action should be silently dropped, not block the stream.
	fake.msgCh <- dockerevents.Message{
		Type:   dockerevents.ContainerEventType,
		Action: dockerevents.ActionTop,
		Actor:  dockerevents.Actor{ID: "c1"},
	}
	fake.msgCh <- dockerevents.Message{
		Type:   dockerevents.ContainerEventType,
		Action: dockerevents.ActionDie,
		Actor:  dockerevents.Actor{ID: "c1", Attributes: map[string]string{"exitCode": "1"}},
	}

	select {
	case ev := <-w.out:
		if ev.Action != "start" {
			t.Errorf("first forwarded event Action = %q, want start", ev.Action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for start event")
	}

	select {
	case ev := <-w.out:
		if ev.Action != "die" {
			t.Errorf("second forwarded event Action = %q, want die", ev.Action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for die event")
	}

	cancel()
	select {
	case err := <-streamDone:
		if err != nil {
			t.Errorf("stream() returned %v on ctx cancellation, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream() did not return after ctx cancellation")
	}
}

func TestWatcherStream_ReturnsErrorFromErrCh(t *testing.T) {
	fake := newFakeEventsSource()
	w := &Watcher{
		nodeID: "node-a",
		client: fake,
		out:    make(chan ContainerEvent, 8),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamDone := make(chan error, 1)
	go func() { streamDone <- w.stream(ctx) }()

	wantErr := errors.New("boom")
	fake.errCh <- wantErr

	select {
	case err := <-streamDone:
		if !errors.Is(err, wantErr) {
			t.Errorf("stream() returned %v, want %v", err, wantErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream() did not return after error")
	}
}

func TestWatcherRun_ClosesOutOnCancel(t *testing.T) {
	fake := newFakeEventsSource()
	w := &Watcher{
		nodeID: "node-a",
		client: fake,
		out:    make(chan ContainerEvent, 8),
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(runDone)
	}()

	cancel()

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}

	// Out() channel must be closed for consumers to detect shutdown.
	select {
	case _, ok := <-w.Out():
		if ok {
			t.Fatal("expected Out() to be closed")
		}
	default:
		// Channel may need a moment to be observed as closed by a
		// concurrent goroutine; give it one more chance.
		select {
		case _, ok := <-w.Out():
			if ok {
				t.Fatal("expected Out() to be closed")
			}
		case <-time.After(time.Second):
			t.Fatal("Out() was not closed")
		}
	}
}

// sanity: filters.Args is used in stream(); ensure import compiles cleanly
// via a trivial construction (guards against accidental removal leaving an
// unused import that a future edit re-adds incorrectly).
type scriptedWatcher struct {
	out chan ContainerEvent
	ev  ContainerEvent
}

func (w *scriptedWatcher) Run(ctx context.Context) {
	select {
	case w.out <- w.ev:
	case <-ctx.Done():
		close(w.out)
		return
	}
	<-ctx.Done()
	close(w.out)
}

func (w *scriptedWatcher) Out() <-chan ContainerEvent { return w.out }

func TestSupervisorSyncAddsAndDropsNodes(t *testing.T) {
	sup := &Supervisor{
		merged:  make(chan ContainerEvent, 4),
		running: map[string]*supervisedNode{},
		factory: func(nodeID string) (eventWatcher, error) {
			if nodeID == "bad" {
				return nil, errors.New("no client")
			}
			return &scriptedWatcher{
				out: make(chan ContainerEvent, 1),
				ev:  ContainerEvent{NodeID: nodeID, Action: "start"},
			}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sup.Sync([]string{"node-a", "node-a", ""}); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-sup.Events():
		if ev.NodeID != "node-a" || ev.Action != "start" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for node-a event")
	}

	if err := sup.Sync([]string{"node-b", "bad"}); err == nil {
		t.Fatal("expected an unresolved node to be reported")
	}
	sup.mu.Lock()
	defer sup.mu.Unlock()
	if _, ok := sup.running["node-a"]; ok {
		t.Fatal("removed node is still watched")
	}
	if _, ok := sup.running["node-b"]; !ok {
		t.Fatal("added node is not watched")
	}
	if _, ok := sup.running["bad"]; ok {
		t.Fatal("unresolved node was watched")
	}
	if len(sup.running) != 1 {
		t.Fatalf("running nodes = %d, want 1", len(sup.running))
	}
}

func TestWatchMembershipReplacesNodes(t *testing.T) {
	var calls atomic.Int32
	sup := &Supervisor{
		merged:  make(chan ContainerEvent, 4),
		running: map[string]*supervisedNode{},
		factory: func(nodeID string) (eventWatcher, error) {
			return &scriptedWatcher{
				out: make(chan ContainerEvent, 1),
				ev:  ContainerEvent{NodeID: nodeID, Action: "start"},
			}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	go sup.WatchMembership(ctx, 15*time.Millisecond, func(context.Context) ([]string, error) {
		if calls.Add(1) == 1 {
			return []string{"a"}, nil
		}
		return []string{"b"}, nil
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sup.mu.Lock()
		_, hasA := sup.running["a"]
		_, hasB := sup.running["b"]
		sup.mu.Unlock()
		if hasB && !hasA {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("membership did not move from a to b")
}

func TestWatchGenerationsReplacesOnEndpointChange(t *testing.T) {
	var lists atomic.Int32
	var built atomic.Int32
	sup := &Supervisor{
		merged:  make(chan ContainerEvent, 4),
		running: map[string]*supervisedNode{},
		factory: func(nodeID string) (eventWatcher, error) {
			built.Add(1)
			return &scriptedWatcher{
				out: make(chan ContainerEvent, 1),
				ev:  ContainerEvent{NodeID: nodeID, Action: "start"},
			}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	go sup.WatchGenerations(ctx, 15*time.Millisecond, func(context.Context) ([]WatchedNode, error) {
		if lists.Add(1) == 1 {
			return []WatchedNode{{ID: "node-a", Generation: 1}}, nil
		}
		return []WatchedNode{{ID: "node-a", Generation: 2}}, nil
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if built.Load() >= 2 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if got := built.Load(); got != 2 {
		t.Fatalf("watchers built = %d, want 2 after the generation change", got)
	}
	time.Sleep(40 * time.Millisecond)
	if got := built.Load(); got != 2 {
		t.Fatalf("watchers built after the new generation settled = %d, want 2", got)
	}
}

func TestSyncGenerationsReplacesTheWatcher(t *testing.T) {
	var calls atomic.Int32
	sup := &Supervisor{
		merged:  make(chan ContainerEvent, 4),
		running: map[string]*supervisedNode{},
		factory: func(nodeID string) (eventWatcher, error) {
			calls.Add(1)
			return &scriptedWatcher{
				out: make(chan ContainerEvent, 1),
				ev:  ContainerEvent{NodeID: nodeID, Action: "start"},
			}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sup.SyncGenerations([]WatchedNode{{ID: "node-a", Generation: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := sup.SyncGenerations([]WatchedNode{{ID: "node-a", Generation: 1}}); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("factory calls after the same generation = %d, want 1", got)
	}
	if err := sup.SyncGenerations([]WatchedNode{{ID: "node-a", Generation: 2}}); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("factory calls after a new generation = %d, want 2", got)
	}
	sup.mu.Lock()
	rw := sup.running["node-a"]
	sup.mu.Unlock()
	if rw == nil || rw.generation != 2 {
		t.Fatalf("running generation = %+v, want 2", rw)
	}
}

func TestUnresolvedNodeIsStaleAndDoesNotBlockAnotherNode(t *testing.T) {
	sup := &Supervisor{
		merged:  make(chan ContainerEvent, 4),
		running: map[string]*supervisedNode{},
		factory: func(nodeID string) (eventWatcher, error) {
			if nodeID == "bad" {
				return nil, errors.New("daemon unreachable")
			}
			return &scriptedWatcher{
				out: make(chan ContainerEvent, 1),
				ev:  ContainerEvent{NodeID: nodeID, Action: "start"},
			}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sup.SyncGenerations([]WatchedNode{{ID: "good", Generation: 1}, {ID: "bad", Generation: 1}}); err == nil {
		t.Fatal("expected the unreachable node to be reported")
	}
	sup.mu.Lock()
	_, goodRunning := sup.running["good"]
	sup.mu.Unlock()
	if !goodRunning {
		t.Fatal("healthy node was not watched")
	}
	stale := sup.StaleNodes()
	if len(stale) != 1 || stale[0] != "bad" {
		t.Fatalf("stale = %v, want [bad]", stale)
	}
	if err := sup.SyncGenerations([]WatchedNode{{ID: "good", Generation: 1}}); err != nil {
		t.Fatal(err)
	}
	if leftover := sup.StaleNodes(); len(leftover) != 0 {
		t.Fatalf("stale after removal = %v", leftover)
	}
}

func TestFullEventBufferDoesNotStallTheWatcher(t *testing.T) {
	watcher := &blockingWatcher{out: make(chan ContainerEvent), sent: make(chan struct{})}
	sup := &Supervisor{
		merged:  make(chan ContainerEvent),
		running: map[string]*supervisedNode{},
		factory: func(string) (eventWatcher, error) { return watcher, nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sup.Sync([]string{"node-a"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-watcher.sent:
	case <-time.After(time.Second):
		t.Fatal("watcher stalled while the merged event buffer was full")
	}
}

type blockingWatcher struct {
	out  chan ContainerEvent
	sent chan struct{}
}

func (w *blockingWatcher) Run(ctx context.Context) {
	w.out <- ContainerEvent{Action: "start"}
	w.out <- ContainerEvent{Action: "die"}
	close(w.sent)
	<-ctx.Done()
	close(w.out)
}

func (w *blockingWatcher) Out() <-chan ContainerEvent { return w.out }

func TestFiltersArgsCompiles(t *testing.T) {
	args := make(client.Filters)
	args.Add("type", "container")
	if _, ok := args["type"]["container"]; !ok {
		t.Fatal("expected client.Filters to contain 'type'")
	}
}
