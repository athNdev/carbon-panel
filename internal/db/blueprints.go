// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package db

import (
	"context"
)

// SaveBlueprint upserts a blueprint by ID.
func (s *Store) SaveBlueprint(ctx context.Context, bp *ServerBlueprint) error {
	return s.db.WithContext(ctx).Save(bp).Error
}

// ListBlueprints returns all blueprints, builtins first.
func (s *Store) ListBlueprints(ctx context.Context) ([]*ServerBlueprint, error) {
	var out []*ServerBlueprint
	return out, s.db.WithContext(ctx).Order("builtin DESC, name ASC").Find(&out).Error
}

// GetBlueprint returns one blueprint by ID.
func (s *Store) GetBlueprint(ctx context.Context, id string) (*ServerBlueprint, error) {
	var bp ServerBlueprint
	if err := s.db.WithContext(ctx).First(&bp, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &bp, nil
}
