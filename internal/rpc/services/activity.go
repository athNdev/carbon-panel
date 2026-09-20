package services

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	"github.com/athNdev/carbon-panel/internal/activity"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ carbonpanelv1connect.ActivityServiceHandler = (*ActivityService)(nil)

// ActivityService serves the audit-trail read API (MINE-141).
type ActivityService struct {
	store *storage.Store
	log   *logger.Logger
}

func NewActivityService(store *storage.Store, log *logger.Logger) *ActivityService {
	return &ActivityService{store: store, log: log}
}

func (s *ActivityService) ListActivityLogs(ctx context.Context, req *connect.Request[v1.ListActivityLogsRequest]) (*connect.Response[v1.ListActivityLogsResponse], error) {
	msg := req.Msg
	entries, err := activity.List(s.store.DB().WithContext(ctx), msg.ServerId, msg.Actor, msg.Since.AsTime(), int(msg.Limit))
	if err != nil {
		s.log.Error("Failed to list activity logs: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list activity logs"))
	}
	out := make([]*v1.ActivityLogEntry, len(entries))
	for i, e := range entries {
		out[i] = &v1.ActivityLogEntry{
			Id: e.ID, ActorId: e.ActorID, ActorName: e.ActorName, Ip: e.IP,
			Event: e.Event, SubjectType: e.SubjectTyp, SubjectId: e.SubjectID,
			Properties: e.Properties, CreatedAt: timestamppb.New(e.CreatedAt),
		}
	}
	return connect.NewResponse(&v1.ListActivityLogsResponse{Entries: out}), nil
}
