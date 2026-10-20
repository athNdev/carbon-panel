// SPDX-License-Identifier: MIT AND MPL-2.0
// This file includes DiscoPanel material under the MIT License,
// Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
// and still applies to that inherited material.
// Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
// If a copy of the MPL was not distributed with this file, You can obtain
// one at https://mozilla.org/MPL/2.0/.

package handlers

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/athNdev/carbon-panel/internal/auth"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/download"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// NewDownloadStreamHandler creates an HTTP handler for streaming file downloads.
//
//	GET /api/v1/download/{sessionId}
//	Auth: Authorization header or cp_session cookie. Query tokens are ignored.
//	Response: file bytes with Content-Disposition, supports Range headers for resume.
func NewDownloadStreamHandler(downloadManager *download.Manager, authManager *auth.Manager, enforcer *rbac.Enforcer, log *logger.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract session ID from path
		sessionID := strings.TrimPrefix(r.URL.Path, "/api/v1/download/")
		if sessionID == "" || strings.Contains(sessionID, "/") {
			http.Error(w, "invalid session_id", http.StatusBadRequest)
			return
		}

		authHeader := ""
		if token := auth.SessionTokenFromRequest(r); token != "" {
			authHeader = "Bearer " + token
		}

		user, err := authManager.AuthenticateFromHeader(r.Context(), authHeader)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Look up the download session BEFORE authorizing. The session records
		// the server it belongs to, so the permission check can be scoped to
		// that server. Enforcing globally first could only ever answer "does
		// this user hold files:read anywhere", which let any such user fetch
		// another user's download if they learned the session id.
		session, err := downloadManager.GetSession(sessionID)
		if err != nil {
			http.Error(w, "download session not found or expired", http.StatusNotFound)
			return
		}

		if !authorizeDownload(enforcer, user, session) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		// Open the temp file
		file, err := os.Open(session.FilePath)
		if err != nil {
			log.Error("Failed to open download file for session %s: %v", sessionID, err)
			http.Error(w, "download file not available", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			log.Error("Failed to stat download file for session %s: %v", sessionID, err)
			http.Error(w, "download file not available", http.StatusInternalServerError)
			return
		}

		// Extend servers write timeout
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Now().Add(30 * time.Minute)); err != nil {
			log.Warn("Failed to set write deadline: %v", err)
		}

		// Set download headers
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, session.Filename))

		// Handles range headers, conditional requests, and Content-Length
		http.ServeContent(w, r, session.Filename, stat.ModTime(), file)
	})
}

// authorizeDownload decides whether user may stream the bytes of session.
//
// The session records the server it belongs to, so the permission check is
// scoped to that server. Sessions created before download sessions carried a
// server id (or when no enforcer is configured) fall back to the global
// check, which is the pre-existing behaviour rather than a new denial.
//
// Extracted from ServeHTTP so the security decision is directly testable
// without standing up an auth manager and a full HTTP round trip.
func authorizeDownload(e *rbac.Enforcer, user *auth.AuthenticatedUser, session *download.Session) bool {
	if e == nil || user == nil || session == nil {
		return false
	}
	objectID := session.ServerID
	if objectID == "" {
		objectID = "*"
	}
	allowed, err := e.Enforce(user.Roles, rbac.ResourceFiles, rbac.ActionRead, objectID)
	return err == nil && allowed
}
