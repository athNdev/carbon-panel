package db

import (
	"context"
	"testing"
)

func TestInvite_UseCountAndDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if err := store.DB().Create(&RegistrationInvite{ID: "inv-1", Code: "C1", CreatedBy: "u1"}).Error; err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := store.IncrementInviteUseCount(ctx, "inv-1"); err != nil {
			t.Fatalf("increment failed: %v", err)
		}
	}
	got, err := store.GetRegistrationInvite(ctx, "inv-1")
	if err != nil || got.UseCount != 3 {
		t.Fatalf("use count = %+v, %v", got, err)
	}

	if err := store.DeleteRegistrationInvite(ctx, "inv-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := store.GetRegistrationInvite(ctx, "inv-1"); err == nil {
		t.Error("deleted invite fetched")
	}
}

func TestSystemSetting_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	if _, err := store.GetSystemSetting(ctx, "missing"); err == nil {
		t.Error("missing setting fetched")
	}
	if err := store.SetSystemSetting(ctx, "k", "v1"); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	got, err := store.GetSystemSetting(ctx, "k")
	if err != nil || got != "v1" {
		t.Fatalf("get = %q, %v", got, err)
	}
	if err := store.SetSystemSetting(ctx, "k", "v2"); err != nil {
		t.Fatalf("overwrite failed: %v", err)
	}
	got, err = store.GetSystemSetting(ctx, "k")
	if err != nil || got != "v2" {
		t.Fatalf("after overwrite = %q, %v", got, err)
	}
}

func TestScheduledTask_CRUD(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	defer store.Close()

	mk := func(id, serverID string) *ScheduledTask {
		return &ScheduledTask{
			ID: id, ServerID: serverID, Name: "n",
			TaskType: TaskTypeBackup, Status: TaskStatusEnabled,
			Schedule: ScheduleTypeInterval, IntervalSecs: 3600,
		}
	}
	for _, task := range []*ScheduledTask{mk("t1", "srv-1"), mk("t2", "srv-1"), mk("t3", "srv-2")} {
		if err := store.CreateScheduledTask(ctx, task); err != nil {
			t.Fatalf("create failed: %v", err)
		}
	}

	got, err := store.GetScheduledTask(ctx, "t1")
	if err != nil || got.Name != "n" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := store.GetScheduledTask(ctx, "missing"); err == nil {
		t.Error("missing task fetched")
	}

	listed, err := store.ListScheduledTasks(ctx, "srv-1")
	if err != nil || len(listed) != 2 {
		t.Fatalf("per-server list = %d, %v", len(listed), err)
	}
	all, err := store.ListAllScheduledTasks(ctx)
	if err != nil || len(all) != 3 {
		t.Fatalf("all list = %d, %v", len(all), err)
	}

	got.Status = TaskStatusDisabled
	if err := store.UpdateScheduledTask(ctx, got); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	after, err := store.GetScheduledTask(ctx, "t1")
	if err != nil || after.Status != TaskStatusDisabled {
		t.Fatalf("after update = %+v, %v", got, err)
	}
}
