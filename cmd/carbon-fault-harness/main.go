// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// carbon-fault-harness is an explicit, opt-in runner for isolated Docker
// integration environments. It never selects a Docker context implicitly.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/athNdev/carbon-panel/internal/testharness"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fault harness:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: carbon-fault-harness <init|run|cleanup> [flags]")
	}
	switch args[0] {
	case "init":
		return initialize(args[1:], false)
	case "run":
		return initialize(args[1:], true)
	case "cleanup":
		return cleanup(args[1:])
	default:
		return fmt.Errorf("unknown command %q; use init, run, or cleanup", args[0])
	}
}

func initialize(args []string, runCases bool) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	a := fs.String("endpoint-a", "", "first explicit Docker Engine API endpoint")
	b := fs.String("endpoint-b", "", "second explicit Docker Engine API endpoint")
	installID := fs.String("install-id", "", "isolated test installation identity")
	nodeID := fs.String("node-id", "", "isolated test node identity")
	runID := fs.String("run-id", "", "run identity; generated when omitted")
	manifestPath := fs.String("manifest", "", "new ownership manifest path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *a == "" || *b == "" || *installID == "" || *nodeID == "" || *manifestPath == "" {
		return errors.New("missing prerequisite: --endpoint-a, --endpoint-b, --install-id, --node-id, and --manifest are required")
	}
	if *runID == "" {
		var err error
		*runID, err = newRunID()
		if err != nil {
			return fmt.Errorf("create run ID: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	manifest, err := testharness.NewManifest(ctx, *a, *b, *installID, *nodeID, *runID)
	if err != nil {
		return err
	}
	if err := testharness.WriteManifest(*manifestPath, manifest); err != nil {
		return fmt.Errorf("write ownership manifest: %w", err)
	}
	fmt.Printf("qualified two distinct Docker daemons and roots; run=%s install=%s node=%s resources=0 manifest=%s\n", manifest.RunID, manifest.InstallID, manifest.NodeID, *manifestPath)
	if runCases {
		return errors.New("no real-daemon cases are registered; refusing to report success with zero cases run (manifest retained for review)")
	}
	fmt.Println("preflight only; no integration cases were run")
	return nil
}

func cleanup(args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	path := fs.String("manifest", "", "ownership manifest created by init/run")
	a := fs.String("endpoint-a", "", "explicit endpoint-a Docker Engine API endpoint")
	b := fs.String("endpoint-b", "", "explicit endpoint-b Docker Engine API endpoint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("missing prerequisite: --manifest is required")
	}
	manifest, err := testharness.ReadManifest(*path)
	if err != nil {
		return fmt.Errorf("read ownership manifest: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := testharness.Cleanup(ctx, manifest, map[string]string{"endpoint-a": *a, "endpoint-b": *b}); err != nil {
		return err
	}
	fmt.Printf("removed only %d manifest-listed resource(s) for run %s; no prune was performed\n", len(manifest.Resources), manifest.RunID)
	return nil
}

func newRunID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
