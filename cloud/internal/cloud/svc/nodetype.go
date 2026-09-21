package svc

import (
	"context"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/provider"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

// NodeTypeService implements NodeTypeServiceHandler over nodetype.Catalog
// (global table) and provider.List() descriptors.
type NodeTypeService struct {
	deps Deps
}

func nodeTypeToProto(t *db.NodeType) *v1.NodeType {
	return &v1.NodeType{
		Id:              t.ID,
		Name:            t.Name,
		Vcpu:            int32(t.VCPU),
		RamMb:           int64(t.RAMMB),
		DiskGb:          int32(t.DiskGB),
		MonthlyPriceUsd: t.MonthlyPriceUSD,
		Description:     t.Description,
		SortOrder:       int32(t.SortOrder),
		Enabled:         t.Enabled,
		InstanceTypes:   providerInstanceTypes(t.InstanceTypeMap()),
	}
}

func providerInstanceTypes(m map[string]string) []*v1.ProviderInstanceType {
	out := make([]*v1.ProviderInstanceType, 0, len(m))
	for provider, instance := range m {
		out = append(out, &v1.ProviderInstanceType{Provider: providerToProto(provider), InstanceType: instance})
	}
	return out
}

// ListNodeTypes lists the catalog; disabled entries need include_disabled.
func (s *NodeTypeService) ListNodeTypes(ctx context.Context, req *connect.Request[v1.ListNodeTypesRequest]) (*connect.Response[v1.ListNodeTypesResponse], error) {
	var rows []db.NodeType
	var err error
	if req.Msg.IncludeDisabled {
		rows, err = s.deps.Catalog.List(ctx)
	} else {
		rows, err = s.deps.Catalog.Enabled(ctx)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errNodeTypes)
	}
	out := make([]*v1.NodeType, 0, len(rows))
	for i := range rows {
		out = append(out, nodeTypeToProto(&rows[i]))
	}
	return connect.NewResponse(&v1.ListNodeTypesResponse{NodeTypes: out}), nil
}

// GetNodeType returns one catalog entry by id.
func (s *NodeTypeService) GetNodeType(ctx context.Context, req *connect.Request[v1.GetNodeTypeRequest]) (*connect.Response[v1.GetNodeTypeResponse], error) {
	t, err := s.deps.Catalog.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errNodeTypeNotFound)
	}
	return connect.NewResponse(&v1.GetNodeTypeResponse{NodeType: nodeTypeToProto(&t)}), nil
}

// ListProviders returns provider descriptors. Configured/MissingKeys reflect
// static credential metadata only; secret values are never read here.
func (s *NodeTypeService) ListProviders(ctx context.Context, req *connect.Request[v1.ListProvidersRequest]) (*connect.Response[v1.ListProvidersResponse], error) {
	descs := provider.List()
	out := make([]*v1.Provider, 0, len(descs))
	for _, d := range descs {
		regions := make([]string, 0, len(d.Regions))
		for _, r := range d.Regions {
			regions = append(regions, r.Name)
		}
		out = append(out, &v1.Provider{
			Provider:     providerToProto(d.Name),
			Name:         d.Title,
			Configured:   false,
			Regions:      regions,
			RequiredKeys: d.CredentialKeys,
			MissingKeys:  d.CredentialKeys,
		})
	}
	return connect.NewResponse(&v1.ListProvidersResponse{Providers: out}), nil
}
