// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package db

import (
	"context"
	"testing"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestModuleLifecycle_Lists(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mk := func(id string, autoStart, detached, follow bool) {
		if err := store.DB().Create(&Module{
			ID: "mod-" + id, Name: id, ServerID: "srv-1", TemplateID: "tpl",
			AutoStart: autoStart, Detached: detached, FollowServerLifecycle: follow,
		}).Error; err != nil {
			t.Fatalf("seed %s failed: %v", id, err)
		}
	}
	mk("auto", true, false, true)
	mk("detached", true, true, true)
	mk("manual", false, false, false)

	auto, err := store.ListAutoStartModules(ctx)
	if err != nil {
		t.Fatalf("autostart failed: %v", err)
	}
	if len(auto) != 1 || auto[0].ID != "mod-auto" {
		t.Errorf("autostart = %v (detached must be excluded)", auto)
	}

	follow, err := store.ListModulesFollowingServerLifecycle(ctx, "srv-1")
	if err != nil {
		t.Fatalf("lifecycle failed: %v", err)
	}
	// All three follow: follow_server_lifecycle defaults true at the DB
	// layer, so even the manual module follows unless detached... note
	// Detached does NOT exclude here (that filtering happens in the
	// lifecycle stop path, not the query).
	if len(follow) != 3 {
		t.Errorf("lifecycle = %d, want 3", len(follow))
	}
	follow, err = store.ListModulesFollowingServerLifecycle(ctx, "srv-other")
	if err != nil || len(follow) != 0 {
		t.Errorf("other server = %d, %v", len(follow), err)
	}
}

func TestEventTriggeredTasks_Filter(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mk := func(id string, schedule ScheduleType, triggers []v1.TriggeredEventType) {
		task := &ScheduledTask{
			ID: "evt-" + id, ServerID: "srv-1", Name: id,
			TaskType: TaskTypeBackup, Status: TaskStatusEnabled, Schedule: schedule,
			EventTriggers: triggers,
		}
		if err := store.DB().Create(task).Error; err != nil {
			t.Fatalf("seed %s failed: %v", id, err)
		}
	}
	mk("cron", ScheduleTypeInterval, nil)
	mk("start-hook", ScheduleTypeEvent, []v1.TriggeredEventType{v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START})
	mk("stop-hook", ScheduleTypeEvent, []v1.TriggeredEventType{v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP})

	matching, err := store.ListEventTriggeredTasks(ctx, "srv-1", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START)
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	if len(matching) != 1 || matching[0].ID != "evt-start-hook" {
		t.Errorf("matching = %v", matching)
	}
	none, err := store.ListEventTriggeredTasks(ctx, "srv-1", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_RESTART)
	if err != nil || len(none) != 0 {
		t.Errorf("restart matching = %v, %v", none, err)
	}
	other, err := store.ListEventTriggeredTasks(ctx, "srv-other", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START)
	if err != nil || len(other) != 0 {
		t.Errorf("other server = %v, %v", other, err)
	}
}

func TestSnapshotStore_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	snap := &ServerSnapshot{ID: "snap-1", ServerID: "srv-1", Name: "pre-update", FilePath: "/backups/s.zip"}
	if err := store.DB().Create(snap).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	got, err := store.GetSnapshot(ctx, "snap-1")
	if err != nil || got.Name != "pre-update" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetSnapshot(ctx, "missing"); err == nil {
		t.Error("missing snapshot fetched")
	}

	listed, err := store.ListSnapshots(ctx, "srv-1")
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %d, %v", len(listed), err)
	}

	if err := store.DeleteSnapshot(ctx, "snap-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.GetSnapshot(ctx, "snap-1"); err == nil {
		t.Error("deleted snapshot fetched")
	}
}
