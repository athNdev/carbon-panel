package services

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
)

var _ carbonpanelv1connect.BlueprintServiceHandler = (*BlueprintService)(nil)

// BlueprintService serves server flavor seeds (MINE-143).
type BlueprintService struct {
	store *storage.Store
	log   *logger.Logger
}

func NewBlueprintService(store *storage.Store, log *logger.Logger) *BlueprintService {
	return &BlueprintService{store: store, log: log}
}

func dbBlueprintToProto(b *storage.ServerBlueprint) *v1.ServerBlueprint {
	return &v1.ServerBlueprint{
		Id: b.ID, Name: b.Name, Description: b.Description,
		ModLoader: string(b.ModLoader), McVersion: b.MCVersion,
		DockerImage: b.DockerImage, DefaultEnv: b.DefaultEnv, Builtin: b.Builtin,
	}
}

func (s *BlueprintService) ListBlueprints(ctx context.Context, _ *connect.Request[v1.ListBlueprintsRequest]) (*connect.Response[v1.ListBlueprintsResponse], error) {
	bps, err := s.store.ListBlueprints(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list blueprints"))
	}
	out := make([]*v1.ServerBlueprint, 0, len(bps))
	for _, b := range bps {
		out = append(out, dbBlueprintToProto(b))
	}
	return connect.NewResponse(&v1.ListBlueprintsResponse{Blueprints: out}), nil
}

func (s *BlueprintService) GetBlueprint(ctx context.Context, req *connect.Request[v1.GetBlueprintRequest]) (*connect.Response[v1.GetBlueprintResponse], error) {
	bp, err := s.store.GetBlueprint(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("blueprint not found"))
	}
	return connect.NewResponse(&v1.GetBlueprintResponse{Blueprint: dbBlueprintToProto(bp)}), nil
}
