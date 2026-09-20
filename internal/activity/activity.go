package activity

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

// Event names written by the audit trail (MINE-141).
const (
	EventAuthLogin    = "auth.login"
	EventAuthLogout   = "auth.logout"
	EventServerStart  = "server.start"
	EventServerStop   = "server.stop"
	EventServerCreate = "server.create"
	EventServerDelete = "server.delete"
)

// Entry is a single audit record.
type Entry struct {
	ActorID    string
	ActorName  string
	IP         string
	Event      string
	SubjectTyp string
	SubjectID  string
	Properties string
}

// Log appends one audit record. Failures are returned; callers log and
// continue — auditing must never break the operation being audited.
func Log(db *gorm.DB, e Entry) error {
	return db.Create(&storage.ActivityLog{
		ID:         uuid.New().String(),
		ActorID:    e.ActorID,
		ActorName:  e.ActorName,
		IP:         e.IP,
		Event:      e.Event,
		SubjectTyp: e.SubjectTyp,
		SubjectID:  e.SubjectID,
		Properties: e.Properties,
	}).Error
}

// Prune deletes records older than olderThan, always keeping at least
// minKeep newest rows. Returns rows deleted.
func Prune(db *gorm.DB, olderThan time.Time, minKeep int) (int64, error) {
	var keepIDs []string
	if err := db.Model(&storage.ActivityLog{}).
		Order("created_at DESC").
		Limit(minKeep).
		Pluck("id", &keepIDs).Error; err != nil {
		return 0, err
	}
	res := db.Where("created_at < ?", olderThan)
	if len(keepIDs) > 0 {
		res = res.Where("id NOT IN ?", keepIDs)
	}
	res = res.Delete(&storage.ActivityLog{})
	return res.RowsAffected, res.Error
}

// List returns newest-first records filtered by server, actor and time.
func List(db *gorm.DB, serverID, actor string, since time.Time, limit int) ([]storage.ActivityLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := db.Model(&storage.ActivityLog{}).Order("created_at DESC").Limit(limit)
	if serverID != "" {
		q = q.Where("subject_type = ? AND subject_id = ?", "server", serverID)
	}
	if actor != "" {
		q = q.Where("actor_id = ? OR actor_name = ?", actor, actor)
	}
	if !since.IsZero() {
		q = q.Where("created_at >= ?", since)
	}
	var out []storage.ActivityLog
	return out, q.Find(&out).Error
}
