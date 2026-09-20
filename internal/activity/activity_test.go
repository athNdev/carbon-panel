package activity

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	if err := db.AutoMigrate(&storage.ActivityLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM activity_logs")
	})
	return db
}

func TestLogAndList(t *testing.T) {
	db := testDB(t)
	if err := Log(db, Entry{ActorID: "u1", ActorName: "op", IP: "1.2.3.4", Event: EventServerStart, SubjectTyp: "server", SubjectID: "s1"}); err != nil {
		t.Fatalf("log: %v", err)
	}
	if err := Log(db, Entry{ActorID: "u2", Event: EventAuthLogin, SubjectTyp: "user", SubjectID: "u2"}); err != nil {
		t.Fatalf("log: %v", err)
	}
	got, err := List(db, "s1", "", time.Time{}, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Event != EventServerStart {
		t.Fatalf("expected 1 server.start record, got %+v", got)
	}
	got, err = List(db, "", "u2", time.Time{}, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("expected 1 record for u2, got %+v err=%v", got, err)
	}
}

func TestPruneKeepsMinimum(t *testing.T) {
	db := testDB(t)
	old := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 5; i++ {
		rec := storage.ActivityLog{ID: string(rune('a' + i)), Event: "test", CreatedAt: old}
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	deleted, err := Prune(db, time.Now().Add(-24*time.Hour), 2)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("expected 3 deleted, got %d", deleted)
	}
	var n int64
	db.Model(&storage.ActivityLog{}).Count(&n)
	if n != 2 {
		t.Fatalf("expected 2 kept, got %d", n)
	}
}
