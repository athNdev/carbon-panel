package db

import (
	"context"
	"encoding/json"
	"fmt"
)

// UpsertSubuser grants (or replaces) scoped permissions for a user on a server.
// Permissions are "resource.action" strings, e.g. "servers.start".
func (s *Store) UpsertSubuser(ctx context.Context, serverID, userID string, permissions []string) error {
	if serverID == "" || userID == "" {
		return fmt.Errorf("server_id and user_id are required")
	}
	for _, p := range permissions {
		if !validSubuserPermission(p) {
			return fmt.Errorf("invalid permission %q: want resource.action", p)
		}
	}
	raw, err := json.Marshal(permissions)
	if err != nil {
		return err
	}
	if _, err := s.GetServer(ctx, serverID); err != nil {
		return fmt.Errorf("server not found: %w", err)
	}
	return s.db.WithContext(ctx).Save(&ServerSubuser{
		ServerID: serverID, UserID: userID, Permissions: string(raw),
	}).Error
}

func validSubuserPermission(p string) bool {
	for i := 0; i < len(p); i++ {
		if p[i] == '.' {
			return i > 0 && i < len(p)-1
		}
	}
	return false
}

// RemoveSubuser revokes a user's grant on a server.
func (s *Store) RemoveSubuser(ctx context.Context, serverID, userID string) error {
	return s.db.WithContext(ctx).
		Where("server_id = ? AND user_id = ?", serverID, userID).
		Delete(&ServerSubuser{}).Error
}

// ListSubusers returns all grants on a server.
func (s *Store) ListSubusers(ctx context.Context, serverID string) ([]*ServerSubuser, error) {
	var out []*ServerSubuser
	return out, s.db.WithContext(ctx).Where("server_id = ?", serverID).Find(&out).Error
}

// SubuserPermissions returns the granted "resource.action" list, or nil.
func (s *Store) SubuserPermissions(ctx context.Context, serverID, userID string) []string {
	var sub ServerSubuser
	if err := s.db.WithContext(ctx).
		Where("server_id = ? AND user_id = ?", serverID, userID).
		First(&sub).Error; err != nil {
		return nil
	}
	var perms []string
	if err := json.Unmarshal([]byte(sub.Permissions), &perms); err != nil {
		return nil
	}
	return perms
}
