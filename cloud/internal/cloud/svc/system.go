package svc

import (
	"context"
	"runtime"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

// SystemService implements SystemServiceHandler: build info and capability
// reporting. Both RPCs are public (see rbac.PublicProcedures).
type SystemService struct {
	deps Deps
}

// GetCapabilities reports which features are configured. Capability values
// come from keys: names only, never secret values.
func (s *SystemService) GetCapabilities(ctx context.Context, req *connect.Request[v1.GetCapabilitiesRequest]) (*connect.Response[v1.GetCapabilitiesResponse], error) {
	caps := make([]*v1.Capability, 0, len(keys.Capabilities()))
	for _, c := range keys.Capabilities() {
		caps = append(caps, &v1.Capability{
			Id:           c,
			Enabled:      false,
			RequiredKeys: keys.RequiredFor(c),
		})
	}
	return connect.NewResponse(&v1.GetCapabilitiesResponse{Capabilities: caps}), nil
}

// GetBuildInfo returns build and runtime metadata from deps (or package vars fallback).
func (s *SystemService) GetBuildInfo(ctx context.Context, req *connect.Request[v1.GetBuildInfoRequest]) (*connect.Response[v1.GetBuildInfoResponse], error) {
	ver := s.deps.Version
	if ver == "" {
		ver = Version
	}
	com := s.deps.Commit
	if com == "" {
		com = Commit
	}
	bt := s.deps.BuildTime
	if bt == "" {
		bt = BuildTime
	}
	return connect.NewResponse(&v1.GetBuildInfoResponse{
		Build: &v1.BuildInfo{
			Version:   ver,
			Commit:    com,
			BuildTime: bt,
			GoVersion: runtime.Version(),
		},
	}), nil
}
