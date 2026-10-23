// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package testharness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/moby/client"
)

const ManifestVersion = 1

const (
	LabelRun     = "io.carbon-panel.fault-harness.run"
	LabelInstall = "io.carbon-panel.fault-harness.install"
	LabelNode    = "io.carbon-panel.fault-harness.node"
)

// Manifest records the owner and daemon identity for every disposable resource.
// Endpoints are deliberately kept outside the file because they can contain
// credentials; callers supply endpoints again when cleaning up.
type Manifest struct {
	Version   int             `json:"version"`
	RunID     string          `json:"run_id"`
	InstallID string          `json:"install_id"`
	NodeID    string          `json:"node_id"`
	CreatedAt time.Time       `json:"created_at"`
	Daemons   []Daemon        `json:"daemons"`
	Resources []OwnedResource `json:"resources"`
}

// Daemon is a qualified Engine identity. Alias maps to endpoint-a or endpoint-b.
type Daemon struct {
	Alias         string `json:"alias"`
	ID            string `json:"id"`
	DockerRootDir string `json:"docker_root_dir"`
	ServerVersion string `json:"server_version"`
}

// OwnedResource identifies one exact Docker object eligible for scoped cleanup.
type OwnedResource struct {
	DaemonAlias string            `json:"daemon_alias"`
	Kind        string            `json:"kind"`
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	Labels      map[string]string `json:"labels"`
}

// NewManifest verifies both endpoints are available, identify different daemons,
// and report different Docker roots before returning a manifest.
func NewManifest(ctx context.Context, endpointA, endpointB, installID, nodeID, runID string) (*Manifest, error) {
	if strings.TrimSpace(installID) == "" || strings.TrimSpace(nodeID) == "" || strings.TrimSpace(runID) == "" {
		return nil, errors.New("install-id, node-id, and run-id are required")
	}
	a, err := inspectDaemon(ctx, "endpoint-a", endpointA)
	if err != nil {
		return nil, fmt.Errorf("endpoint-a prerequisite unavailable: %w", err)
	}
	b, err := inspectDaemon(ctx, "endpoint-b", endpointB)
	if err != nil {
		return nil, fmt.Errorf("endpoint-b prerequisite unavailable: %w", err)
	}
	if a.ID == b.ID {
		return nil, fmt.Errorf("isolation prerequisite failed: both endpoints report daemon ID %q", a.ID)
	}
	if a.DockerRootDir == "" || b.DockerRootDir == "" || filepath.Clean(a.DockerRootDir) == filepath.Clean(b.DockerRootDir) {
		return nil, fmt.Errorf("isolation prerequisite failed: Docker roots must be present and distinct (A=%q, B=%q)", a.DockerRootDir, b.DockerRootDir)
	}
	return &Manifest{Version: ManifestVersion, RunID: runID, InstallID: installID, NodeID: nodeID, CreatedAt: time.Now().UTC(), Daemons: []Daemon{a, b}, Resources: []OwnedResource{}}, nil
}

func inspectDaemon(ctx context.Context, alias, endpoint string) (Daemon, error) {
	if strings.TrimSpace(endpoint) == "" {
		return Daemon{}, errors.New("endpoint is empty")
	}
	cli, err := client.New(client.WithHost(endpoint))
	if err != nil {
		return Daemon{}, fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = cli.Close() }()
	info, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return Daemon{}, err
	}
	if strings.TrimSpace(info.Info.ID) == "" {
		return Daemon{}, errors.New("daemon returned an empty ID")
	}
	return Daemon{Alias: alias, ID: info.Info.ID, DockerRootDir: info.Info.DockerRootDir, ServerVersion: info.Info.ServerVersion}, nil
}

// WriteManifest writes atomically and refuses to overwrite an existing run file.
func WriteManifest(path string, manifest *Manifest) error {
	if manifest == nil || manifest.Version != ManifestVersion {
		return errors.New("invalid manifest")
	}
	if len(manifest.Daemons) != 2 || manifest.RunID == "" || manifest.InstallID == "" || manifest.NodeID == "" {
		return errors.New("manifest is missing run, install, node, or daemon identity")
	}
	if manifest.Daemons[0].ID == "" || manifest.Daemons[1].ID == "" || manifest.Daemons[0].ID == manifest.Daemons[1].ID || manifest.Daemons[0].DockerRootDir == "" || manifest.Daemons[1].DockerRootDir == "" || filepath.Clean(manifest.Daemons[0].DockerRootDir) == filepath.Clean(manifest.Daemons[1].DockerRootDir) {
		return errors.New("manifest daemons must have distinct IDs and isolated Docker roots")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("manifest already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fault-manifest-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return nil
}

// ReadManifest parses and validates the version and required ownership fields.
func ReadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.Version != ManifestVersion || manifest.RunID == "" || manifest.InstallID == "" || manifest.NodeID == "" || len(manifest.Daemons) != 2 || manifest.Daemons[0].ID == "" || manifest.Daemons[1].ID == "" || manifest.Daemons[0].ID == manifest.Daemons[1].ID || manifest.Daemons[0].DockerRootDir == "" || manifest.Daemons[1].DockerRootDir == "" || filepath.Clean(manifest.Daemons[0].DockerRootDir) == filepath.Clean(manifest.Daemons[1].DockerRootDir) {
		return nil, errors.New("manifest has incomplete ownership or daemon identity")
	}
	return &manifest, nil
}

// OwnershipLabels returns the complete identifying label set for an owned object.
func (m *Manifest) OwnershipLabels() map[string]string {
	return map[string]string{LabelRun: m.RunID, LabelInstall: m.InstallID, LabelNode: m.NodeID}
}

// AddResource records an exact object only when its ownership labels match this run.
func (m *Manifest) AddResource(resource OwnedResource) error {
	if resource.ID == "" || (resource.DaemonAlias != "endpoint-a" && resource.DaemonAlias != "endpoint-b") {
		return errors.New("resource must have an exact ID and a known daemon alias")
	}
	for key, value := range m.OwnershipLabels() {
		if resource.Labels[key] != value {
			return fmt.Errorf("resource ownership label %s does not match run", key)
		}
	}
	if resource.Kind != "container" && resource.Kind != "network" && resource.Kind != "volume" {
		return fmt.Errorf("unsupported cleanup kind %q", resource.Kind)
	}
	m.Resources = append(m.Resources, resource)
	return nil
}

// Cleanup removes only IDs listed in the manifest after confirming each object's
// daemon identity and all three ownership labels. It never prunes or enumerates.
func Cleanup(ctx context.Context, manifest *Manifest, endpoints map[string]string) error {
	if manifest == nil {
		return errors.New("manifest is required")
	}
	daemons := make(map[string]Daemon, len(manifest.Daemons))
	for _, daemon := range manifest.Daemons {
		daemons[daemon.Alias] = daemon
	}
	if daemons["endpoint-a"].ID == "" || daemons["endpoint-b"].ID == "" || daemons["endpoint-a"].ID == daemons["endpoint-b"].ID || filepath.Clean(daemons["endpoint-a"].DockerRootDir) == filepath.Clean(daemons["endpoint-b"].DockerRootDir) {
		return errors.New("refusing cleanup: manifest does not identify two isolated Docker daemons")
	}
	clients := make(map[string]*client.Client)
	defer func() {
		for _, cli := range clients {
			_ = cli.Close()
		}
	}()
	// Verify both endpoints up front, including for an empty resource list.
	for _, alias := range []string{"endpoint-a", "endpoint-b"} {
		endpoint := endpoints[alias]
		if endpoint == "" {
			return fmt.Errorf("endpoint for %s is required to clean manifest run %q", alias, manifest.RunID)
		}
		cli, err := client.New(client.WithHost(endpoint))
		if err != nil {
			return fmt.Errorf("connect %s: %w", alias, err)
		}
		clients[alias] = cli
		info, err := cli.Info(ctx, client.InfoOptions{})
		if err != nil {
			return fmt.Errorf("verify %s: %w", alias, err)
		}
		d := daemons[alias]
		if info.Info.ID != d.ID || filepath.Clean(info.Info.DockerRootDir) != filepath.Clean(d.DockerRootDir) {
			return fmt.Errorf("refusing cleanup: %s daemon identity/root changed", alias)
		}
	}
	for _, resource := range manifest.Resources {
		_, ok := daemons[resource.DaemonAlias]
		if !ok {
			return fmt.Errorf("resource %q names unknown daemon %q", resource.ID, resource.DaemonAlias)
		}
		cli := clients[resource.DaemonAlias]
		labels, err := inspectResourceLabels(ctx, cli, resource)
		if err != nil {
			return fmt.Errorf("verify resource %s %q: %w", resource.Kind, resource.ID, err)
		}
		for key, value := range manifest.OwnershipLabels() {
			if labels[key] != value {
				return fmt.Errorf("refusing cleanup of %s %q: ownership label %s mismatch", resource.Kind, resource.ID, key)
			}
		}
		switch resource.Kind {
		case "container":
			_, err = cli.ContainerRemove(ctx, resource.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: false})
		case "network":
			_, err = cli.NetworkRemove(ctx, resource.ID, client.NetworkRemoveOptions{})
		case "volume":
			_, err = cli.VolumeRemove(ctx, resource.ID, client.VolumeRemoveOptions{Force: true})
		default:
			err = fmt.Errorf("unsupported cleanup kind %q", resource.Kind)
		}
		if err != nil {
			return fmt.Errorf("remove owned %s %q: %w", resource.Kind, resource.ID, err)
		}
	}
	return nil
}

func inspectResourceLabels(ctx context.Context, cli *client.Client, resource OwnedResource) (map[string]string, error) {
	switch resource.Kind {
	case "container":
		got, err := cli.ContainerInspect(ctx, resource.ID, client.ContainerInspectOptions{})
		if err != nil {
			return nil, err
		}
		if got.Container.Config == nil {
			return nil, errors.New("container has no config labels")
		}
		return got.Container.Config.Labels, nil
	case "network":
		got, err := cli.NetworkInspect(ctx, resource.ID, client.NetworkInspectOptions{})
		if err != nil {
			return nil, err
		}
		return got.Network.Labels, nil
	case "volume":
		got, err := cli.VolumeInspect(ctx, resource.ID, client.VolumeInspectOptions{})
		if err != nil {
			return nil, err
		}
		return got.Volume.Labels, nil
	default:
		return nil, fmt.Errorf("unsupported cleanup kind %q", resource.Kind)
	}
}
