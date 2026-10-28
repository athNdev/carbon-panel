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
// remove, or prune containers. release-port frees host ports left behind by
// a detached server and does not stop a container or delete world files.
func runOperationsCommand(ctx context.Context, store *storage.Store, command, id, note, serverID string, out io.Writer) error {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "list":
		return writeOperationList(ctx, store, out)
	case "ports":
		return writePortLeases(ctx, store, out)
	case "release-port":
		return releaseDetachedPorts(ctx, store, serverID, out)
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
		return fmt.Errorf("unknown -operations command %q (use list, resolve, ports, or release-port)", strings.TrimSpace(command))
	}
}

func writeOperationList(ctx context.Context, store *storage.Store, out io.Writer) error {
	ops, err := store.ListServerOperations(ctx)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		fmt.Fprintln(out, "no server operations")
		return nil
	}
	fmt.Fprintln(out, "ID\tSERVER\tKIND\tPHASE\tACTIVE\tUPDATED\tDETAIL")
	for _, op := range ops {
		active := "no"
		if op.ActiveServer != nil {
			active = "yes"
		}
		detail := strings.ReplaceAll(op.LastError, "\n", " ")
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			op.ID, op.ServerID, op.Kind, op.Phase, active, op.UpdatedAt.UTC().Format(time.RFC3339), detail)
	}
	return nil
}

func writePortLeases(ctx context.Context, store *storage.Store, out io.Writer) error {
	leases, err := store.ListBackendPortLeases(ctx)
	if err != nil {
		return err
	}
	if len(leases) == 0 {
		_, err := fmt.Fprintln(out, "no port leases")
		return err
	}
	rows, err := store.ListServers(ctx)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(rows))
	for _, server := range rows {
		if server != nil {
			present[server.ID] = true
		}
	}
	if _, err := fmt.Fprintln(out, "NODE\tPORT\tSERVER\tPANEL_ROW"); err != nil {
		return err
	}
	for _, lease := range leases {
		panelRow := "no"
		if present[lease.ServerID] {
			panelRow = "yes"
		}
		if _, err := fmt.Fprintf(out, "%s\t%d\t%s\t%s\n", lease.NodeID, lease.Port, lease.ServerID, panelRow); err != nil {
			return err
		}
	}
	return nil
}

func releaseDetachedPorts(ctx context.Context, store *storage.Store, serverID string, out io.Writer) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return errors.New("-operations release-port requires -server-id")
	}
	released, err := store.ReleaseDetachedBackendPorts(ctx, serverID)
	if err != nil {
		return err
	}
	if len(released) == 0 {
		_, err := fmt.Fprintf(out, "no port leases for %s\n", serverID)
		return err
	}
	ports := make([]string, 0, len(released))
	for _, lease := range released {
		ports = append(ports, fmt.Sprintf("%s:%d", lease.NodeID, lease.Port))
	}
	_, err = fmt.Fprintf(out, "released %s for %s without stopping a container or deleting files\n", strings.Join(ports, ", "), serverID)
	return err
}
