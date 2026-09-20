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

var _ carbonpanelv1connect.SubuserServiceHandler = (*SubuserService)(nil)

// SubuserService manages per-server grants (MINE-139).
type SubuserService struct {
	store *storage.Store
	log   *logger.Logger
}

func NewSubuserService(store *storage.Store, log *logger.Logger) *SubuserService {
	return &SubuserService{store: store, log: log}
}

func (s *SubuserService) ListSubusers(ctx context.Context, req *connect.Request[v1.ListSubusersRequest]) (*connect.Response[v1.ListSubusersResponse], error) {
	subs, err := s.store.ListSubusers(ctx, req.Msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list subusers"))
	}
	out := make([]*v1.SubuserGrant, 0, len(subs))
	for _, sub := range subs {
		username := ""
		if u, err := s.store.GetUser(ctx, sub.UserID); err == nil && u != nil {
			username = u.Username
		}
		out = append(out, &v1.SubuserGrant{
			ServerId: sub.ServerID, UserId: sub.UserID,
			Username: username, Permissions: s.store.SubuserPermissions(ctx, sub.ServerID, sub.UserID),
		})
	}
	return connect.NewResponse(&v1.ListSubusersResponse{Grants: out}), nil
}

func (s *SubuserService) SetSubuser(ctx context.Context, req *connect.Request[v1.SetSubuserRequest]) (*connect.Response[v1.SetSubuserResponse], error) {
	msg := req.Msg
	if _, err := s.store.GetUser(ctx, msg.UserId); err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("user not found"))
	}
	if err := s.store.UpsertSubuser(ctx, msg.ServerId, msg.UserId, msg.Permissions); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&v1.SetSubuserResponse{Grant: &v1.SubuserGrant{
		ServerId: msg.ServerId, UserId: msg.UserId, Permissions: msg.Permissions,
	}}), nil
}

func (s *SubuserService) RemoveSubuser(ctx context.Context, req *connect.Request[v1.RemoveSubuserRequest]) (*connect.Response[v1.RemoveSubuserResponse], error) {
	if err := s.store.RemoveSubuser(ctx, req.Msg.ServerId, req.Msg.UserId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to remove subuser"))
	}
	return connect.NewResponse(&v1.RemoveSubuserResponse{Success: true}), nil
}
