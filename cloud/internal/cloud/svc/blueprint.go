package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/blueprint"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
)

// BlueprintService implements cloudv1connect.BlueprintServiceHandler (MINE-162).
type BlueprintService struct {
	cloudv1connect.UnimplementedBlueprintServiceHandler
	deps Deps
}

// NewBlueprintService creates a new BlueprintService.
func NewBlueprintService(deps Deps) *BlueprintService {
	return &BlueprintService{deps: deps}
}

func (s *BlueprintService) ListBlueprints(ctx context.Context, req *connect.Request[v1.ListBlueprintsRequest]) (*connect.Response[v1.ListBlueprintsResponse], error) {
	filterLoader := strings.ToLower(strings.TrimSpace(req.Msg.Loader))
	var out []*v1.Blueprint

	// 1. Builtins first
	for _, b := range blueprint.BuiltinBlueprints() {
		if filterLoader == "" || strings.ToLower(b.Loader) == filterLoader {
			out = append(out, b)
		}
	}

	// 2. Organization custom blueprints
	q, err := s.deps.Store.Org(ctx)
	if err == nil {
		var custom []db.Blueprint
		dbQuery := q.Model(&db.Blueprint{})
		if filterLoader != "" {
			dbQuery = dbQuery.Where("LOWER(loader) = ?", filterLoader)
		}
		if err := dbQuery.Order("created_at DESC").Find(&custom).Error; err == nil {
			for _, c := range custom {
				out = append(out, blueprintToProto(&c))
			}
		}
	}

	return connect.NewResponse(&v1.ListBlueprintsResponse{Blueprints: out}), nil
}

func (s *BlueprintService) GetBlueprint(ctx context.Context, req *connect.Request[v1.GetBlueprintRequest]) (*connect.Response[v1.GetBlueprintResponse], error) {
	id := strings.TrimSpace(req.Msg.Id)
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("blueprint id is required"))
	}

	// Check builtins first
	if b := blueprint.FindBuiltin(id); b != nil {
		return connect.NewResponse(&v1.GetBlueprintResponse{Blueprint: b}), nil
	}

	// Check org custom blueprints
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("blueprint not found"))
	}

	var bp db.Blueprint
	if err := q.Where("id = ?", id).First(&bp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("blueprint not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to query blueprint"))
	}

	return connect.NewResponse(&v1.GetBlueprintResponse{Blueprint: blueprintToProto(&bp)}), nil
}

func (s *BlueprintService) CreateBlueprint(ctx context.Context, req *connect.Request[v1.CreateBlueprintRequest]) (*connect.Response[v1.CreateBlueprintResponse], error) {
	name := strings.TrimSpace(req.Msg.Name)
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("blueprint name is required"))
	}
	loader := strings.ToLower(strings.TrimSpace(req.Msg.Loader))
	if loader == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("loader is required"))
	}

	// Prevent shadowing builtins
	if blueprint.FindBuiltin(name) != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("cannot name custom blueprint after a builtin preset"))
	}

	p, ok := principal.From(ctx)
	if !ok || p.OrgID == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("organization context required"))
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("organization context required"))
	}

	mcVer := strings.TrimSpace(req.Msg.MinecraftVersion)
	if mcVer == "" {
		mcVer = "1.21.4"
	}
	dockerImg := strings.TrimSpace(req.Msg.DockerImage)
	if dockerImg == "" {
		dockerImg = "itzg/minecraft-server:latest"
	}

	mem := req.Msg.DefaultMemoryMb
	if mem <= 0 {
		mem = 2048
	}
	cpu := req.Msg.DefaultCpuMillicores
	if cpu <= 0 {
		cpu = 1000
	}

	envJSON, _ := json.Marshal(req.Msg.DefaultEnv)
	jvmJSON, _ := json.Marshal(req.Msg.DefaultJvmFlags)

	record := &db.Blueprint{
		TenantBase: db.TenantBase{
			ID:    uuid.NewString(),
			OrgID: p.OrgID,
		},
		Name:                 name,
		Description:          strings.TrimSpace(req.Msg.Description),
		Loader:               loader,
		MinecraftVersion:     mcVer,
		DockerImage:          dockerImg,
		DefaultMemoryMB:      mem,
		DefaultCPUMillicores: cpu,
		DefaultEnv:           string(envJSON),
		DefaultJVMFlags:      string(jvmJSON),
	}

	if err := q.Create(record).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate key") {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("blueprint with this name already exists in organization"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create blueprint"))
	}

	return connect.NewResponse(&v1.CreateBlueprintResponse{Blueprint: blueprintToProto(record)}), nil
}

func (s *BlueprintService) UpdateBlueprint(ctx context.Context, req *connect.Request[v1.UpdateBlueprintRequest]) (*connect.Response[v1.UpdateBlueprintResponse], error) {
	id := strings.TrimSpace(req.Msg.Id)
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("blueprint id is required"))
	}

	if blueprint.FindBuiltin(id) != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("builtin blueprints are immutable and cannot be modified"))
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("organization context required"))
	}

	var bp db.Blueprint
	if err := q.Where("id = ?", id).First(&bp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("blueprint not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to query blueprint"))
	}

	updates := map[string]any{}
	if req.Msg.Name != nil && strings.TrimSpace(*req.Msg.Name) != "" {
		updates["name"] = strings.TrimSpace(*req.Msg.Name)
	}
	if req.Msg.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Msg.Description)
	}
	if req.Msg.Loader != nil && strings.TrimSpace(*req.Msg.Loader) != "" {
		updates["loader"] = strings.ToLower(strings.TrimSpace(*req.Msg.Loader))
	}
	if req.Msg.MinecraftVersion != nil && strings.TrimSpace(*req.Msg.MinecraftVersion) != "" {
		updates["minecraft_version"] = strings.TrimSpace(*req.Msg.MinecraftVersion)
	}
	if req.Msg.DockerImage != nil && strings.TrimSpace(*req.Msg.DockerImage) != "" {
		updates["docker_image"] = strings.TrimSpace(*req.Msg.DockerImage)
	}
	if req.Msg.DefaultMemoryMb != nil && *req.Msg.DefaultMemoryMb > 0 {
		updates["default_memory_mb"] = *req.Msg.DefaultMemoryMb
	}
	if req.Msg.DefaultCpuMillicores != nil && *req.Msg.DefaultCpuMillicores > 0 {
		updates["default_cpu_millicores"] = *req.Msg.DefaultCpuMillicores
	}
	if req.Msg.DefaultEnv != nil {
		envJSON, _ := json.Marshal(req.Msg.DefaultEnv)
		updates["default_env"] = string(envJSON)
	}
	if req.Msg.DefaultJvmFlags != nil {
		jvmJSON, _ := json.Marshal(req.Msg.DefaultJvmFlags)
		updates["default_jvm_flags"] = string(jvmJSON)
	}

	if len(updates) > 0 {
		uq, err := s.deps.Store.Org(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("organization context required"))
		}
		if err := uq.Model(&db.Blueprint{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update blueprint: %w", err))
		}
		_ = uq.Where("id = ?", id).First(&bp)
	}

	return connect.NewResponse(&v1.UpdateBlueprintResponse{Blueprint: blueprintToProto(&bp)}), nil
}

func (s *BlueprintService) DeleteBlueprint(ctx context.Context, req *connect.Request[v1.DeleteBlueprintRequest]) (*connect.Response[v1.DeleteBlueprintResponse], error) {
	id := strings.TrimSpace(req.Msg.Id)
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("blueprint id is required"))
	}

	if blueprint.FindBuiltin(id) != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("builtin blueprints cannot be deleted"))
	}

	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("organization context required"))
	}

	res := q.Where("id = ?", id).Delete(&db.Blueprint{})
	if res.Error != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to delete blueprint"))
	}
	if res.RowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("blueprint not found"))
	}

	return connect.NewResponse(&v1.DeleteBlueprintResponse{}), nil
}

func blueprintToProto(b *db.Blueprint) *v1.Blueprint {
	var env map[string]string
	if b.DefaultEnv != "" {
		_ = json.Unmarshal([]byte(b.DefaultEnv), &env)
	}
	var flags []string
	if b.DefaultJVMFlags != "" {
		_ = json.Unmarshal([]byte(b.DefaultJVMFlags), &flags)
	}

	return &v1.Blueprint{
		Id:                   b.ID,
		Name:                 b.Name,
		Description:          b.Description,
		Loader:               b.Loader,
		MinecraftVersion:     b.MinecraftVersion,
		DockerImage:          b.DockerImage,
		DefaultMemoryMb:      b.DefaultMemoryMB,
		DefaultCpuMillicores: b.DefaultCPUMillicores,
		DefaultEnv:           env,
		DefaultJvmFlags:      flags,
		Builtin:              false,
		OrgId:                b.OrgID,
		CreatedAt:            timestamppb.New(b.CreatedAt),
		UpdatedAt:            timestamppb.New(b.UpdatedAt),
	}
}
