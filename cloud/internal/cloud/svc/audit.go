package svc

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/audit"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
)

// AuditService implements AuditServiceHandler over audit.Store (append-only,
// org-scoped reads for the caller's org).
type AuditService struct {
	deps Deps
}

func auditEventToProto(e *audit.Event) *v1.AuditEvent {
	detail := ""
	if len(e.Detail) > 0 {
		if raw, err := json.Marshal(e.Detail); err == nil {
			detail = string(raw)
		}
	}
	return &v1.AuditEvent{
		Id:            e.ID,
		OrgId:         e.OrgID,
		ActorUserId:   e.ActorUserID,
		ActorApiKeyId: e.ActorAPIKeyID,
		Action:        e.Action,
		ResourceType:  e.ResourceType,
		ResourceId:    e.ResourceID,
		Result:        e.Result,
		DetailJson:    detail,
		Ip:            e.IP,
		UserAgent:     e.UserAgent,
		CreatedAt:     ts(e.CreatedAt),
	}
}

// ListAuditEvents queries the caller's org audit trail. ActionPrefix maps to
// the store's exact Action match when it names a full action; wildcard
// prefixes are rejected (fail-closed filtering).
func (s *AuditService) ListAuditEvents(ctx context.Context, req *connect.Request[v1.ListAuditEventsRequest]) (*connect.Response[v1.ListAuditEventsResponse], error) {
	m := req.Msg
	if strings.Contains(m.ActionPrefix, "*") {
		return nil, connect.NewError(connect.CodeInvalidArgument, errAuditPrefix)
	}
	f := audit.Filter{
		Action:       strings.TrimSpace(m.ActionPrefix),
		ResourceType: strings.TrimSpace(m.ResourceType),
		ResourceID:   strings.TrimSpace(m.ResourceId),
		ActorUserID:  strings.TrimSpace(m.ActorUserId),
	}
	if v := strings.TrimSpace(m.Since); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = t
		}
	}
	if v := strings.TrimSpace(m.Until); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = t
		}
	}
	limit, offset := page(m.Page, 50)
	f.Limit = limit
	if offset > 0 {
		// The store cursor is a decimal row offset.
		f.Cursor = strconv.Itoa(offset)
	}
	events, next, err := s.deps.Audits.List(ctx, f)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errAuditList)
	}
	out := make([]*v1.AuditEvent, 0, len(events))
	for i := range events {
		out = append(out, auditEventToProto(&events[i]))
	}
	resp := &v1.ListAuditEventsResponse{Events: out, Page: pageResp(limit+offset, limit, offset)}
	if next != "" {
		resp.Page.NextPageToken = next
	}
	return connect.NewResponse(resp), nil
}

// GetAuditEvent returns one audit entry by id inside the caller's org.
func (s *AuditService) GetAuditEvent(ctx context.Context, req *connect.Request[v1.GetAuditEventRequest]) (*connect.Response[v1.GetAuditEventResponse], error) {
	if strings.TrimSpace(req.Msg.Id) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errAuditNoID)
	}
	e, err := s.deps.Audits.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errAuditNotFound)
	}
	return connect.NewResponse(&v1.GetAuditEventResponse{Event: auditEventToProto(&e)}), nil
}
