package svc

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/auth"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	"github.com/google/uuid"
)

// APIKeyService implements ApiKeyServiceHandler against db.ApiKey.
//
// The plaintext secret exists only in the Create/Rotate responses: it is
// returned ONCE and never stored or echoed again. List paths expose
// metadata (id, prefix) only.
type APIKeyService struct {
	deps Deps
}

func apiKeyToProto(k *db.ApiKey) *v1.ApiKey {
	return &v1.ApiKey{
		Id:          k.ID,
		OrgId:       k.OrgID,
		Name:        k.Name,
		Prefix:      k.Prefix,
		Permissions: k.PermissionList(),
		LastUsedAt:  tsPtr(k.LastUsedAt),
		ExpiresAt:   tsPtr(k.ExpiresAt),
		RevokedAt:   tsPtr(k.RevokedAt),
		CreatedAt:   ts(k.CreatedAt),
	}
}

// ListApiKeys lists key metadata for the caller's org.
func (s *APIKeyService) ListApiKeys(ctx context.Context, req *connect.Request[v1.ListApiKeysRequest]) (*connect.Response[v1.ListApiKeysResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	limit, offset := page(req.Msg.Page, 50)
	var total int64
	if err := q.Model(&db.ApiKey{}).Count(&total).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errKeyList)
	}
	var rows []db.ApiKey
	if err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errKeyList)
	}
	out := make([]*v1.ApiKey, 0, len(rows))
	for i := range rows {
		out = append(out, apiKeyToProto(&rows[i]))
	}
	return connect.NewResponse(&v1.ListApiKeysResponse{ApiKeys: out, Page: pageResp(int(total), limit, offset)}), nil
}

// CreateApiKey mints a key. The response carries the secret exactly once.
func (s *APIKeyService) CreateApiKey(ctx context.Context, req *connect.Request[v1.CreateApiKeyRequest]) (*connect.Response[v1.CreateApiKeyResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	name := strings.TrimSpace(req.Msg.Name)
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errKeyName)
	}
	secret, prefix, hash, err := auth.NewAPIKey()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errKeyMint)
	}
	p, _ := principal.From(ctx)
	k := &db.ApiKey{
		TenantBase: db.TenantBase{ID: uuid.NewString(), OrgID: principal.OrgID(ctx)},
		Name:       name,
		Prefix:     prefix,
		Hash:       hash,
		CreatedBy:  p.UserID,
	}
	k.SetPermissions(req.Msg.Permissions)
	if req.Msg.ExpiresInSeconds > 0 {
		exp := time.Now().UTC().Add(time.Duration(req.Msg.ExpiresInSeconds) * time.Second)
		k.ExpiresAt = &exp
	}
	if err := q.Create(k).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errKeyCreate)
	}
	return connect.NewResponse(&v1.CreateApiKeyResponse{ApiKey: apiKeyToProto(k), Secret: secret}), nil
}

// RevokeApiKey marks a key revoked; it stops authenticating immediately.
func (s *APIKeyService) RevokeApiKey(ctx context.Context, req *connect.Request[v1.RevokeApiKeyRequest]) (*connect.Response[v1.RevokeApiKeyResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	if strings.TrimSpace(req.Msg.Id) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errKeyNoID)
	}
	now := time.Now().UTC()
	if err := q.Model(&db.ApiKey{}).Where("id = ?", req.Msg.Id).Update("revoked_at", now).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errKeyRevoke)
	}
	return connect.NewResponse(&v1.RevokeApiKeyResponse{}), nil
}

// RotateApiKey revokes the old key and mints a replacement with the same
// name and permissions. The new secret is returned exactly once.
func (s *APIKeyService) RotateApiKey(ctx context.Context, req *connect.Request[v1.RotateApiKeyRequest]) (*connect.Response[v1.RotateApiKeyResponse], error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errNoOrgCtx)
	}
	if strings.TrimSpace(req.Msg.Id) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errKeyNoID)
	}
	var old db.ApiKey
	if err := q.Where("id = ?", req.Msg.Id).First(&old).Error; err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errKeyNotFound)
	}
	secret, prefix, hash, err := auth.NewAPIKey()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errKeyMint)
	}
	now := time.Now().UTC()
	p, _ := principal.From(ctx)
	next := &db.ApiKey{
		TenantBase:  db.TenantBase{ID: uuid.NewString(), OrgID: principal.OrgID(ctx)},
		Name:        old.Name,
		Prefix:      prefix,
		Hash:        hash,
		Permissions: old.Permissions,
		CreatedBy:   p.UserID,
		ExpiresAt:   old.ExpiresAt,
	}
	if err := q.Create(next).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errKeyCreate)
	}
	if err := q.Model(&db.ApiKey{}).Where("id = ?", old.ID).Update("revoked_at", now).Error; err != nil {
		return nil, connect.NewError(connect.CodeInternal, errKeyRevoke)
	}
	return connect.NewResponse(&v1.RotateApiKeyResponse{ApiKey: apiKeyToProto(next), Secret: secret}), nil
}
