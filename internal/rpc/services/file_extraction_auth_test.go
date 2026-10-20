// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package services

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/internal/auth"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestGetExtractionStatusIsCreatorScoped(t *testing.T) {
	svc := &FileService{}
	svc.extractions.Store("op-1", &extractionOp{
		State:    "completed",
		ServerID: "server-b",
		UserID:   "user-a",
	})

	other := auth.WithUser(context.Background(), &auth.AuthenticatedUser{ID: "user-b"})
	_, err := svc.GetExtractionStatus(other, connect.NewRequest(&v1.GetExtractionStatusRequest{OperationId: "op-1"}))
	if err == nil || connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("cross-server probe code = %v, err = %v", connect.CodeOf(err), err)
	}

	_, err = svc.GetExtractionStatus(context.Background(), connect.NewRequest(&v1.GetExtractionStatusRequest{OperationId: "op-1"}))
	if err == nil || connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("anonymous probe code = %v, err = %v", connect.CodeOf(err), err)
	}

	svc.extractions.Store("legacy", &extractionOp{State: "completed"})
	owner := auth.WithUser(context.Background(), &auth.AuthenticatedUser{ID: "user-a"})
	_, err = svc.GetExtractionStatus(owner, connect.NewRequest(&v1.GetExtractionStatusRequest{OperationId: "legacy"}))
	if err == nil || connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("legacy op code = %v, err = %v", connect.CodeOf(err), err)
	}

	resp, err := svc.GetExtractionStatus(owner, connect.NewRequest(&v1.GetExtractionStatusRequest{OperationId: "op-1"}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.State != "completed" {
		t.Fatalf("state = %q", resp.Msg.State)
	}
}
