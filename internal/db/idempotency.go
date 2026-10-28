// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxRequestIDLen = 128

// ErrRequestIDTooLong is returned when a client request id exceeds the stored limit.
var ErrRequestIDTooLong = errors.New("request_id must be at most 128 characters")

// ErrRequestIDReused is returned when a request id is presented for a different kind.
var ErrRequestIDReused = errors.New("request_id already used for a different operation")

// IdempotencyKey binds a client request id to the server it first created.
// Lifecycle retries of an existing server use the operation journal instead.
type IdempotencyKey struct {
	RequestID string `gorm:"primaryKey;size:128"`
	ServerID  string `gorm:"not null"`
	Kind      string `gorm:"not null"`
	CreatedAt time.Time
}

// ClaimIdempotencyKey records requestID for serverID. The first caller is
// claimed. A repeat of the same kind returns the original server id and
// claimed=false. An empty requestID is always claimed and is not stored.
func (s *Store) ClaimIdempotencyKey(ctx context.Context, requestID, serverID, kind string) (string, bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return serverID, true, nil
	}
	if len(requestID) > maxRequestIDLen {
		return "", false, ErrRequestIDTooLong
	}
	if serverID == "" || kind == "" {
		return "", false, errors.New("idempotency claim requires a server and kind")
	}
	row := IdempotencyKey{RequestID: requestID, ServerID: serverID, Kind: kind}
	err := s.db.WithContext(ctx).Create(&row).Error
	if err == nil {
		return serverID, true, nil
	}
	if !isUniqueViolation(err) {
		return "", false, err
	}
	var existing IdempotencyKey
	if err := s.db.WithContext(ctx).Where("request_id = ?", requestID).First(&existing).Error; err != nil {
		return "", false, err
	}
	if existing.Kind != kind {
		return "", false, fmt.Errorf("%w: %s", ErrRequestIDReused, existing.Kind)
	}
	return existing.ServerID, false, nil
}

// ReleaseIdempotencyKey forgets a claim that never became a server row.
func (s *Store) ReleaseIdempotencyKey(ctx context.Context, requestID string) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	return s.db.WithContext(ctx).Where("request_id = ?", requestID).Delete(&IdempotencyKey{}).Error
}
