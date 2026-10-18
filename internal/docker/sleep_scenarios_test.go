// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScenario1_IdleDetectionAndTransition verifies that an idle server past its timeout
// triggers graceful stop and transitions to db.StatusDeepSleep.
func TestScenario1_IdleDetectionAndTransition(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	require.NoError(t, mgr.CheckIdleServers(ctx))

	mgr.trackerMu.Lock()
	mgr.idleTracker["srv-sleep"] = time.Now().Add(-2 * time.Minute)
	mgr.trackerMu.Unlock()

	require.NoError(t, mgr.CheckIdleServers(ctx))

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusDeepSleep, srv.Status, "Server status must be deepsleep after idle timeout")
}

// TestScenario2_DBStatusPreSet_ProtectsAgainstDieEvent verifies that db.StatusDeepSleep
// is pre-set before the container stops, protecting against reconciler/die-event clobbering.
func TestScenario2_DBStatusPreSet_ProtectsAgainstDieEvent(t *testing.T) {
	ctx := context.Background()
	_, commander, store := newDeepSleepHarness()

	var statusDuringStop db.ServerStatus
	var statusChecked bool

	// Intercept StopContainer to inspect DB status during stop execution
	customController := &mockSleepController{
		status: map[string]db.ServerStatus{"cnt-sleep": db.StatusRunning},
	}
	originalCommander := commander
	originalCommander.onCommand = func(serverID, command string) {
		if command == "stop" {
			srv, err := store.GetServer(ctx, serverID)
			if err == nil {
				statusDuringStop = srv.Status
				statusChecked = true
			}
			customController.status["cnt-sleep"] = db.StatusStopped
		}
	}

	mgr := NewDeepSleepManager(customController, store, originalCommander, nil, nil)
	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))

	assert.True(t, statusChecked, "DB status should be checked during container stop")
	assert.Equal(t, db.StatusDeepSleep, statusDuringStop,
		"DB status MUST be StatusDeepSleep before container stops so reconciler sees desired=deepsleep")
}

type failingStopController struct {
	mockSleepController
}

func (f *failingStopController) StopContainer(ctx context.Context, containerID string) (bool, error) {
	return false, errors.New("simulated docker stop error")
}

type statusErrorSleepController struct {
	mockSleepController
	err error
}

func (c *statusErrorSleepController) GetContainerStatus(context.Context, string) (db.ServerStatus, error) {
	return "", c.err
}

// TestScenario3_StopFailure_RollsBackStatus verifies that if stopping container fails,
// DB status rolls back to StatusRunning and routes are refreshed.
func TestScenario3_StopFailure_RollsBackStatus(t *testing.T) {
	oldTimeout := deepSleepStopTimeout
	oldPoll := deepSleepStopPollInterval
	deepSleepStopTimeout = 20 * time.Millisecond
	deepSleepStopPollInterval = 5 * time.Millisecond
	defer func() {
		deepSleepStopTimeout = oldTimeout
		deepSleepStopPollInterval = oldPoll
	}()

	ctx := context.Background()
	store := newMockFreezerStore()
	store.servers["srv-err"] = &db.Server{
		ID:            "srv-err",
		ContainerID:   "cnt-err",
		Status:        db.StatusRunning,
		AutoDeepSleep: true,
	}

	failingController := &failingStopController{
		mockSleepController: mockSleepController{
			status: map[string]db.ServerStatus{"cnt-err": db.StatusRunning},
		},
	}
	failingCommander := &mockSleepCommander{}

	var refreshedID string
	mgr := NewDeepSleepManager(failingController, store, failingCommander, func(id string) {
		refreshedID = id
	}, nil)

	err := mgr.SleepServer(ctx, "srv-err")
	assert.Error(t, err, "SleepServer must return error when container stop fails")

	srv, err := store.GetServer(ctx, "srv-err")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status, "Status must roll back to StatusRunning on stop failure")
	assert.Equal(t, "srv-err", refreshedID, "Route refresher must be called on rollback")
}

func TestDeepSleep_StatusReadFailureDoesNotClaimAsleep(t *testing.T) {
	ctx := context.Background()
	store := newMockFreezerStore()
	store.servers["srv-status-error"] = &db.Server{
		ID: "srv-status-error", ContainerID: "cnt-status-error", Status: db.StatusRunning,
	}
	controller := &statusErrorSleepController{
		mockSleepController: mockSleepController{status: map[string]db.ServerStatus{"cnt-status-error": db.StatusRunning}},
		err:                 errors.New("temporary Docker inspect failure"),
	}
	refreshed := false
	mgr := NewDeepSleepManager(controller, store, nil, func(string) { refreshed = true }, nil)

	err := mgr.SleepServer(ctx, "srv-status-error")
	assert.Error(t, err, "SleepServer must report an unknown container state")

	srv, getErr := store.GetServer(ctx, "srv-status-error")
	require.NoError(t, getErr)
	assert.Equal(t, db.StatusRunning, srv.Status, "unknown Docker state must not be persisted as deep sleep")
	assert.True(t, refreshed, "proxy route should be restored after the failed sleep attempt")
}

// TestScenario4_IsTransitioning_StateFlag verifies that IsTransitioning correctly reports
// true during sleep transition and false once finished.
func TestScenario4_IsTransitioning_StateFlag(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	stopGate := make(chan struct{})
	transitionObserved := false

	commander.onCommand = func(serverID, command string) {
		if command == "stop" {
			<-stopGate
			controller.mu.Lock()
			controller.status["cnt-sleep"] = db.StatusStopped
			controller.mu.Unlock()
		}
	}

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)

	done := make(chan error, 1)
	go func() {
		done <- mgr.SleepServer(ctx, "srv-sleep")
	}()

	require.Eventually(t, func() bool {
		transitionObserved = mgr.IsTransitioning("srv-sleep")
		return transitionObserved
	}, 1*time.Second, 10*time.Millisecond, "IsTransitioning must be true while sleep is active")

	close(stopGate)
	require.NoError(t, <-done)

	assert.False(t, mgr.IsTransitioning("srv-sleep"), "IsTransitioning must be false after completion")
}

// TestScenario5_GracefulRCONStopPrecedesForceStop verifies save-all and stop are dispatched.
func TestScenario5_GracefulRCONStopPrecedesForceStop(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	var commandOrder []string
	commander.onCommand = func(serverID, command string) {
		commandOrder = append(commandOrder, command)
		if command == "stop" {
			controller.mu.Lock()
			controller.status["cnt-sleep"] = db.StatusStopped
			controller.mu.Unlock()
		}
	}

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))

	require.Len(t, commandOrder, 2)
	assert.Equal(t, "save-all", commandOrder[0], "save-all must be called first to flush world chunks")
	assert.Equal(t, "stop", commandOrder[1], "stop must be called second to safely exit JVM")
}

// TestScenario6_WakeOperation_StartsContainerAndRestoresRunning verifies WakeServer.
func TestScenario6_WakeOperation_StartsContainerAndRestoresRunning(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	var refreshedID string
	mgr := NewDeepSleepManager(controller, store, commander, func(id string) {
		refreshedID = id
	}, nil)

	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))
	require.NoError(t, mgr.WakeServer(ctx, "srv-sleep"))

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status, "Server status must be restored to StatusRunning")
	assert.Equal(t, "srv-sleep", refreshedID, "Route refresher must be called after wake")
}

// TestScenario7_WakeSingleFlight_Deduplication verifies concurrent wakes deduplicate boots.
func TestScenario7_WakeSingleFlight_Deduplication(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()
	controller.startGate = make(chan struct{})

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))

	var wg sync.WaitGroup
	errs := make([]error, 3)

	for i := 0; i < 3; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[idx] = mgr.WakeServer(ctx, "srv-sleep")
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(controller.startGate)
	wg.Wait()

	for _, err := range errs {
		assert.NoError(t, err, "All concurrent wakes must complete without error")
	}

	controller.mu.Lock()
	defer controller.mu.Unlock()
	assert.Len(t, controller.starts, 1, "Container should have been started exactly once")
}

// TestScenario8_PlayerActivity_PostponesSleep verifies that active player count prevents sleep.
func TestScenario8_PlayerActivity_PostponesSleep(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	mgr.SetPlayerCountProvider(func(serverID string) int {
		return 1
	})

	mgr.trackerMu.Lock()
	mgr.idleTracker["srv-sleep"] = time.Now().Add(-10 * time.Minute)
	mgr.trackerMu.Unlock()

	require.NoError(t, mgr.CheckIdleServers(ctx))

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status, "Server with active players must NOT sleep")
}

// TestScenario9_RefresherInvoked_MaintainsDownRoute verifies refresher is called on sleep.
func TestScenario9_RefresherInvoked_MaintainsDownRoute(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	refresherCalled := false
	mgr := NewDeepSleepManager(controller, store, commander, func(id string) {
		if id == "srv-sleep" {
			refresherCalled = true
		}
	}, nil)

	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))
	assert.True(t, refresherCalled, "Refresher callback must be invoked when server sleeps")
}

// TestScenario10_WakeRejectsUnknownServer verifies error handling on unknown server wake.
func TestScenario10_WakeRejectsUnknownServer(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	err := mgr.WakeServer(ctx, "nonexistent-server")
	assert.Error(t, err, "Waking nonexistent server must return error")
}

// TestScenario11_ContextCancel_RollsBackPreSetStatus covers the abort path: the
// status is pre-set to DeepSleep before the container is stopped, so a cancelled
// context must roll it back. Otherwise the server is wedged — the reconciler
// refuses to heal a DeepSleep server and the proxy route stays down, so nothing
// can revive it without a manual database edit.
func TestScenario11_ContextCancel_RollsBackPreSetStatus(t *testing.T) {
	// The container stays alive, so the stop-poll loop runs until the deadline
	// or cancellation rather than exiting on its own. Stub out the RCON stop
	// side effect so the harness does not simulate a JVM that exited promptly.
	controller, _, store := newDeepSleepHarness()
	stubCommander := &mockSleepCommander{}
	controller.mu.Lock()
	controller.status["cnt-sleep"] = db.StatusRunning
	controller.mu.Unlock()

	cancelCtx, cancel := context.WithCancel(context.Background())

	var refreshedID string
	mgr := NewDeepSleepManager(controller, store, stubCommander, func(id string) {
		refreshedID = id
	}, nil)

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := mgr.SleepServer(cancelCtx, "srv-sleep")
	require.Error(t, err, "a cancelled context must abort SleepServer")
	assert.ErrorIs(t, err, context.Canceled)

	srv, err := store.GetServer(context.Background(), "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status,
		"status must roll back to running: the container never stopped, so DeepSleep would be a lie")
	assert.Equal(t, "srv-sleep", refreshedID, "rollback must refresh the proxy route so the backend returns")
}

// TestScenario12_RollbackSleep_IsSafeOnCancelledContext verifies the rollback
// write itself survives a dead context, using a detached context internally.
// Without that, the compensating write would fail exactly when it is needed.
func TestScenario12_RollbackSleep_IsSafeOnCancelledContext(t *testing.T) {
	controller, commander, store := newDeepSleepHarness()

	var refreshedID string
	mgr := NewDeepSleepManager(controller, store, commander, func(id string) {
		refreshedID = id
	}, nil)

	// Put the server into the pre-set state, then roll back with a dead context.
	require.NoError(t, store.UpdateServerStatus(context.Background(), "srv-sleep", db.StatusDeepSleep))

	deadCtx, cancel := context.WithCancel(context.Background())
	cancel()
	mgr.rollbackSleep(deadCtx, "srv-sleep")

	srv, err := store.GetServer(context.Background(), "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status,
		"rollback must persist even when the caller's context is already cancelled")
	assert.Equal(t, "srv-sleep", refreshedID, "rollback must refresh the proxy route")
}
