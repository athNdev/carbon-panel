// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

// runOperationsCommand inspects or releases lifecycle journal locks and exits.
// Resolve records an operator note and clears the lock. It does not stop,
// remove, or prune containers.
func runOperationsCommand(ctx context.Context, store *storage.Store, command, id, note string, out io.Writer) error {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "list":
		return writeOperationList(ctx, store, out)
	case "resolve":
		id = strings.TrimSpace(id)
		if id == "" {
			return errors.New("-operations resolve requires -operation-id")
		}
		op, err := store.GetServerOperation(ctx, id)
		if err != nil {
			return err
		}
		if op.ActiveServer == nil {
			fmt.Fprintf(out, "operation %s is already released\n", op.ID)
			return nil
		}
		if err := store.ResolveServerOperation(ctx, id, note); err != nil {
			return err
		}
		fmt.Fprintf(out, "resolved %s without changing containers\n", op.ID)
		return nil
	default:
		return fmt.Errorf("unknown -operations command %q (use list or resolve)", strings.TrimSpace(command))
	}
}

func writeOperationList(ctx context.Context, store *storage.Store, out io.Writer) error {
	ops, err := store.ListServerOperations(ctx)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		_, err := fmt.Fprintln(out, "no server operations")
		return err
	}
	if _, err := fmt.Fprintln(out, "ID\tSERVER\tKIND\tPHASE\tACTIVE\tNODE\tGENERATION\tDAEMON\tUPDATED\tDETAIL"); err != nil {
		return err
	}
	for _, op := range ops {
		active := "no"
		if op.ActiveServer != nil {
			active = "yes"
		}
		detail := strings.ReplaceAll(op.LastError, "\n", " ")
		if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			op.ID, op.ServerID, op.Kind, op.Phase, active, op.NodeID, op.EndpointGeneration, op.DaemonID, op.UpdatedAt.UTC().Format(time.RFC3339), detail); err != nil {
			return err
		}
	}
	return nil
}
