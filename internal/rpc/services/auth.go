package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/crypto/bcrypt"

	"github.com/athNdev/carbon-panel/internal/activity"
	"github.com/athNdev/carbon-panel/internal/auth"
	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/rbac"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1/carbonpanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ carbonpanelv1connect.AuthServiceHandler = (*AuthService)(nil)

type AuthService struct {
	store       *storage.Store
	authManager *auth.Manager
	enforcer    *rbac.Enforcer
	oidcHandler *auth.OIDCHandler
	log         *logger.Logger

	// userThrottle keys on the attempted username (tight ceiling: targeted
	// guessing). ipThrottle keys on the client IP with a looser ceiling, so a
	// spray from one address cannot lock out every user sharing a NAT or the
	// reverse proxy.
	userThrottle *auth.LoginThrottle
	ipThrottle   *auth.LoginThrottle
}

func NewAuthService(store *storage.Store, authManager *auth.Manager, enforcer *rbac.Enforcer, oidcHandler *auth.OIDCHandler, log *logger.Logger) *AuthService {
	return &AuthService{
		store:        store,
		authManager:  authManager,
		enforcer:     enforcer,
		oidcHandler:  oidcHandler,
		log:          log,
		userThrottle: auth.NewLoginThrottleWithLimits(5, 5*time.Minute, 15*time.Minute),
		ipThrottle:   auth.NewLoginThrottleWithLimits(30, 5*time.Minute, 15*time.Minute),
	}
}

func (s *AuthService) GetAuthStatus(ctx context.Context, req *connect.Request[v1.GetAuthStatusRequest]) (*connect.Response[v1.GetAuthStatusResponse], error) {
	userCount, err := s.store.CountUsers(ctx)
	if err != nil {
		s.log.Error("Failed to count users: %v", err)
		userCount = 0
	}

	oidcEnabled := s.oidcHandler != nil && s.oidcHandler.IsEnabled()

	return connect.NewResponse(&v1.GetAuthStatusResponse{
		LocalAuthEnabled:       s.authManager.IsLocalAuthEnabled(),
		OidcEnabled:            oidcEnabled,
		AllowRegistration:      s.authManager.IsRegistrationAllowed(),
		FirstUserSetup:         userCount == 0,
		AnonymousAccessEnabled: s.authManager.IsAnonymousAccessEnabled(),
	}), nil
}

func (s *AuthService) Login(ctx context.Context, req *connect.Request[v1.LoginRequest]) (*connect.Response[v1.LoginResponse], error) {
	msg := req.Msg

	if msg.Username == "" || msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	// Throttle online password guessing: tight per-username, looser per-IP.
	ipKey := "ip:" + realClientIP(req.Header(), req.Peer().Addr)
	userKey := "user:" + strings.ToLower(msg.Username)
	if allowed, retryAfter := s.userThrottle.Allow(userKey); !allowed {
		return nil, throttledError(retryAfter)
	}
	if allowed, retryAfter := s.ipThrottle.Allow(ipKey); !allowed {
		return nil, throttledError(retryAfter)
	}

	user, roles, token, expiresAt, err := s.authManager.Login(ctx, msg.Username, msg.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrUserNotActive) {
			// Record the failure against both keys before answering, so a
			// single attacker cannot rotate usernames or source ports for free.
			s.userThrottle.Failure(userKey)
			s.ipThrottle.Failure(ipKey)
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
		}
		if errors.Is(err, auth.ErrLocalAuthDisabled) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("local authentication is disabled"))
		}
		s.log.Error("Login failed: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("login failed"))
	}

	s.userThrottle.Reset(userKey)
	s.ipThrottle.Reset(ipKey)

	// Audit trail (MINE-141): never break login on audit failure.
	if aerr := activity.Log(s.store.DB(), activity.Entry{
		ActorID: user.ID, ActorName: user.Username, IP: req.Peer().Addr,
		Event: activity.EventAuthLogin, SubjectTyp: "user", SubjectID: user.ID,
	}); aerr != nil {
		s.log.Error("Audit log failed for login: %v", aerr)
	}

	return connect.NewResponse(&v1.LoginResponse{
		Token:     token,
		User:      dbUserToProto(user, roles),
		ExpiresAt: timestamppb.New(expiresAt),
	}), nil
}

// clientIP strips the port from a peer address so throttle keys are stable per
// client rather than per connection.
func clientIP(peerAddr string) string {
	if host, _, err := net.SplitHostPort(peerAddr); err == nil {
		return host
	}
	return peerAddr
}

// realClientIP resolves the client address for throttling. When the direct peer
// is a trusted (loopback or private) proxy it honours X-Forwarded-For /
// X-Real-IP, otherwise it uses the peer address. A LAN client can therefore
// spoof the IP key, but the per-username limiter still applies.
func realClientIP(hdr http.Header, peerAddr string) string {
	peer := clientIP(peerAddr)
	if ip := net.ParseIP(peer); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if xff := hdr.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
		if xri := strings.TrimSpace(hdr.Get("X-Real-IP")); xri != "" {
			return xri
		}
	}
	return peer
}

// throttledError builds a resource_exhausted error and advertises the retry
// window via the standard Retry-After header.
func throttledError(retryAfter time.Duration) error {
	seconds := int(retryAfter / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	err := connect.NewError(connect.CodeResourceExhausted, errors.New("too many failed login attempts; try again later"))
	err.Meta().Set("Retry-After", strconv.Itoa(seconds))
	return err
}

func (s *AuthService) Logout(ctx context.Context, req *connect.Request[v1.LogoutRequest]) (*connect.Response[v1.LogoutResponse], error) {
	// Extract token from Authorization header
	token := ""
	if authHeader := req.Header().Get("Authorization"); authHeader != "" {
		token, _ = strings.CutPrefix(authHeader, "Bearer ")
		token, _ = strings.CutPrefix(token, "bearer ")
	}

	if token != "" {
		if err := s.authManager.Logout(ctx, token); err != nil {
			s.log.Debug("Logout error: %v", err)
		}
	}

	return connect.NewResponse(&v1.LogoutResponse{
		Message: "logged out",
	}), nil
}

func (s *AuthService) Register(ctx context.Context, req *connect.Request[v1.RegisterRequest]) (*connect.Response[v1.RegisterResponse], error) {
	msg := req.Msg

	userCount, _ := s.store.CountUsers(ctx)
	isFirstUser := userCount == 0

	var invite *storage.RegistrationInvite

	if msg.InviteCode != nil && *msg.InviteCode != "" {
		// Validate invite
		var err error
		invite, err = s.store.GetRegistrationInviteByCode(ctx, *msg.InviteCode)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("invalid invite code"))
		}
		if invite.ExpiresAt != nil && invite.ExpiresAt.Before(time.Now()) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invite has expired"))
		}
		if invite.MaxUses > 0 && invite.UseCount >= invite.MaxUses {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invite has reached maximum uses"))
		}
		if invite.PinHash != "" {
			if msg.InvitePin == nil || *msg.InvitePin == "" {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("PIN is required for this invite"))
			}
			if err := bcrypt.CompareHashAndPassword([]byte(invite.PinHash), []byte(*msg.InvitePin)); err != nil {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("incorrect PIN"))
			}
		}
	} else if !isFirstUser && !s.authManager.IsRegistrationAllowed() {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("registration is disabled"))
	}

	if msg.Username == "" || msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	user, err := s.authManager.CreateLocalUser(ctx, msg.Username, msg.Email, msg.Password)
	if err != nil {
		if errors.Is(err, auth.ErrPasswordTooWeak) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		s.log.Error("Registration failed: %v", err)
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("registration failed"))
	}

	// Role assignment: first user â†’ admin; invite with roles â†’ invite roles; else â†’ default roles
	if isFirstUser {
		_ = s.store.AssignRole(ctx, user.ID, "admin", "local")
	} else if invite != nil && len(invite.Roles) > 0 {
		for _, roleName := range invite.Roles {
			_ = s.store.AssignRole(ctx, user.ID, roleName, "invite")
		}
	} else {
		defaultRoles, _ := s.store.GetDefaultRoles(ctx)
		for _, role := range defaultRoles {
			_ = s.store.AssignRole(ctx, user.ID, role.Name, "local")
		}
	}

	// Increment invite use count after successful registration
	if invite != nil {
		_ = s.store.IncrementInviteUseCount(ctx, invite.ID)
	}

	roles, _ := s.store.GetUserRoleNames(ctx, user.ID)

	return connect.NewResponse(&v1.RegisterResponse{
		User: dbUserToProto(user, roles),
	}), nil
}

func (s *AuthService) GetCurrentUser(ctx context.Context, req *connect.Request[v1.GetCurrentUserRequest]) (*connect.Response[v1.GetCurrentUserResponse], error) {
	authUser := auth.GetUserFromContext(ctx)
	if authUser == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}

	// When all auth is disabled or when browsing anonymously, return synthetic permissions
	if !s.authManager.IsAnyAuthEnabled() || authUser.Provider == "anonymous" {
		roles := authUser.Roles
		var protoUser *v1.User
		if authUser.Provider != "anonymous" {
			protoUser = &v1.User{
				Id:           authUser.ID,
				Username:     authUser.Username,
				AuthProvider: authUser.Provider,
				IsActive:     true,
				Roles:        roles,
			}
		}

		var permissions []*v1.Permission
		if s.enforcer != nil {
			for _, role := range roles {
				for _, p := range s.enforcer.GetPermissionsForRole(role) {
					permissions = append(permissions, &v1.Permission{
						Resource: p.Resource,
						Action:   p.Action,
						ObjectId: p.ObjectID,
					})
				}
			}
		}

		return connect.NewResponse(&v1.GetCurrentUserResponse{
			User:        protoUser,
			Permissions: permissions,
		}), nil
	}

	// Fetch user from db
	dbUser, err := s.store.GetUser(ctx, authUser.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get user"))
	}

	// Fetch roles from db
	roles, _ := s.store.GetUserRoleNames(ctx, authUser.ID)

	// Collect permissions from all user roles via the RBAC enforcer
	var permissions []*v1.Permission
	if s.enforcer != nil {
		seen := make(map[string]bool)
		for _, role := range roles {
			for _, p := range s.enforcer.GetPermissionsForRole(role) {
				key := p.Resource + ":" + p.Action + ":" + p.ObjectID
				if !seen[key] {
					seen[key] = true
					permissions = append(permissions, &v1.Permission{
						Resource: p.Resource,
						Action:   p.Action,
						ObjectId: p.ObjectID,
					})
				}
			}
		}
	}

	return connect.NewResponse(&v1.GetCurrentUserResponse{
		User:        dbUserToProto(dbUser, roles),
		Permissions: permissions,
	}), nil
}

func (s *AuthService) ChangePassword(ctx context.Context, req *connect.Request[v1.ChangePasswordRequest]) (*connect.Response[v1.ChangePasswordResponse], error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}

	msg := req.Msg
	if msg.OldPassword == "" || msg.NewPassword == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("old and new passwords are required"))
	}

	if err := s.authManager.ChangePassword(ctx, user.ID, msg.OldPassword, msg.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("incorrect current password"))
		}
		if errors.Is(err, auth.ErrPasswordTooWeak) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		s.log.Error("Change password failed: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to change password"))
	}

	// Invalidate every other session for this user so a stolen token cannot
	// outlive the credential rotation. The caller's own token is kept so their
	// current session is not logged out.
	currentToken := ""
	if authHeader := req.Header().Get("Authorization"); authHeader != "" {
		currentToken, _ = strings.CutPrefix(authHeader, "Bearer ")
		currentToken, _ = strings.CutPrefix(currentToken, "bearer ")
	}
	if err := s.authManager.InvalidateUserSessions(ctx, user.ID, currentToken); err != nil {
		s.log.Error("Failed to invalidate sessions after password change for %s: %v", user.ID, err)
	}

	return connect.NewResponse(&v1.ChangePasswordResponse{
		Message: "password changed",
	}), nil
}

func (s *AuthService) GetOIDCLoginURL(ctx context.Context, req *connect.Request[v1.GetOIDCLoginURLRequest]) (*connect.Response[v1.GetOIDCLoginURLResponse], error) {
	if s.oidcHandler == nil || !s.oidcHandler.IsEnabled() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("OIDC is not enabled"))
	}

	return connect.NewResponse(&v1.GetOIDCLoginURLResponse{
		LoginUrl: "/api/v1/auth/oidc/login",
	}), nil
}

func (s *AuthService) GetAuthConfig(ctx context.Context, req *connect.Request[v1.GetAuthConfigRequest]) (*connect.Response[v1.GetAuthConfigResponse], error) {
	cfg := s.authManager.GetConfig()
	oidcEnabled := s.oidcHandler != nil && s.oidcHandler.IsEnabled()

	userCount, err := s.store.CountUsers(ctx)
	if err != nil {
		s.log.Error("Failed to count users: %v", err)
		userCount = 0
	}

	resp := &v1.GetAuthConfigResponse{
		LocalAuthEnabled:  cfg.Local.Enabled,
		AllowRegistration: cfg.Local.AllowRegistration,
		AnonymousAccess:   cfg.AnonymousAccess,
		SessionTimeout:    int32(cfg.SessionTimeout),
		OidcEnabled:       oidcEnabled,
		FirstUserSetup:    userCount == 0,
	}

	if oidcEnabled {
		resp.OidcIssuerUri = &cfg.OIDC.IssuerURI
		resp.OidcClientId = &cfg.OIDC.ClientID
		resp.OidcRedirectUrl = &cfg.OIDC.RedirectURL
		resp.OidcScopes = cfg.OIDC.Scopes
		resp.OidcRoleClaim = &cfg.OIDC.RoleClaim
	}

	return connect.NewResponse(resp), nil
}

func (s *AuthService) UpdateAuthSettings(ctx context.Context, req *connect.Request[v1.UpdateAuthSettingsRequest]) (*connect.Response[v1.UpdateAuthSettingsResponse], error) {
	msg := req.Msg

	if err := s.authManager.UpdateSettings(ctx, msg.LocalAuthEnabled, msg.AllowRegistration, msg.AnonymousAccess, msg.SessionTimeout); err != nil {
		if errors.Is(err, auth.ErrSessionTimeoutMin) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if errors.Is(err, auth.ErrNoAuthProvider) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		s.log.Error("Failed to update auth settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update auth settings"))
	}

	// Return the updated config
	configResp, err := s.GetAuthConfig(ctx, connect.NewRequest(&v1.GetAuthConfigRequest{}))
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&v1.UpdateAuthSettingsResponse{
		Config: configResp.Msg,
	}), nil
}

func (s *AuthService) CreateInvite(ctx context.Context, req *connect.Request[v1.CreateInviteRequest]) (*connect.Response[v1.CreateInviteResponse], error) {
	msg := req.Msg

	// Validate roles exist
	if len(msg.Roles) > 0 {
		existingRoles, err := s.store.ListRoles(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list roles"))
		}
		roleSet := make(map[string]bool, len(existingRoles))
		for _, r := range existingRoles {
			roleSet[r.Name] = true
		}
		for _, roleName := range msg.Roles {
			if !roleSet[roleName] {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role not found: "+roleName))
			}
		}
	}

	// Generate crypto-random code
	codeBytes := make([]byte, 32)
	if _, err := rand.Read(codeBytes); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to generate invite code"))
	}
	code := base64.RawURLEncoding.EncodeToString(codeBytes)

	// Hash PIN if provided
	var pinHash string
	if msg.Pin != nil && *msg.Pin != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*msg.Pin), bcrypt.DefaultCost)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to hash PIN"))
		}
		pinHash = string(hash)
	}

	// Calculate expiration
	var expiresAt *time.Time
	if msg.ExpiresInHours != nil && *msg.ExpiresInHours > 0 {
		t := time.Now().Add(time.Duration(*msg.ExpiresInHours) * time.Hour)
		expiresAt = &t
	}

	// Get creator from context
	authUser := auth.GetUserFromContext(ctx)
	createdBy := ""
	if authUser != nil {
		createdBy = authUser.Username
	}

	invite := &storage.RegistrationInvite{
		Code:        code,
		Description: msg.Description,
		Roles:       msg.Roles,
		PinHash:     pinHash,
		MaxUses:     int(msg.MaxUses),
		ExpiresAt:   expiresAt,
		CreatedBy:   createdBy,
	}

	if err := s.store.CreateRegistrationInvite(ctx, invite); err != nil {
		s.log.Error("Failed to create invite: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create invite"))
	}

	return connect.NewResponse(&v1.CreateInviteResponse{
		Invite: dbInviteToProto(invite),
	}), nil
}

func (s *AuthService) ListInvites(ctx context.Context, req *connect.Request[v1.ListInvitesRequest]) (*connect.Response[v1.ListInvitesResponse], error) {
	invites, err := s.store.ListRegistrationInvites(ctx)
	if err != nil {
		s.log.Error("Failed to list invites: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list invites"))
	}

	protoInvites := make([]*v1.RegistrationInvite, 0, len(invites))
	for _, inv := range invites {
		protoInvites = append(protoInvites, dbInviteToProto(inv))
	}

	return connect.NewResponse(&v1.ListInvitesResponse{
		Invites: protoInvites,
	}), nil
}

func (s *AuthService) GetInvite(ctx context.Context, req *connect.Request[v1.GetInviteRequest]) (*connect.Response[v1.GetInviteResponse], error) {
	invite, err := s.store.GetRegistrationInvite(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("invite not found"))
	}

	return connect.NewResponse(&v1.GetInviteResponse{
		Invite: dbInviteToProto(invite),
	}), nil
}

func (s *AuthService) DeleteInvite(ctx context.Context, req *connect.Request[v1.DeleteInviteRequest]) (*connect.Response[v1.DeleteInviteResponse], error) {
	if err := s.store.DeleteRegistrationInvite(ctx, req.Msg.Id); err != nil {
		s.log.Error("Failed to delete invite: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to delete invite"))
	}

	return connect.NewResponse(&v1.DeleteInviteResponse{}), nil
}

func (s *AuthService) ValidateInvite(ctx context.Context, req *connect.Request[v1.ValidateInviteRequest]) (*connect.Response[v1.ValidateInviteResponse], error) {
	if req.Msg.Code == "" {
		return connect.NewResponse(&v1.ValidateInviteResponse{Valid: false}), nil
	}

	invite, err := s.store.GetRegistrationInviteByCode(ctx, req.Msg.Code)
	if err != nil {
		return connect.NewResponse(&v1.ValidateInviteResponse{Valid: false}), nil
	}

	if invite.ExpiresAt != nil && invite.ExpiresAt.Before(time.Now()) {
		return connect.NewResponse(&v1.ValidateInviteResponse{Valid: false}), nil
	}

	if invite.MaxUses > 0 && invite.UseCount >= invite.MaxUses {
		return connect.NewResponse(&v1.ValidateInviteResponse{Valid: false}), nil
	}

	return connect.NewResponse(&v1.ValidateInviteResponse{
		Valid:       true,
		RequiresPin: invite.PinHash != "",
		Description: invite.Description,
	}), nil
}

func (s *AuthService) CreateAPIToken(ctx context.Context, req *connect.Request[v1.CreateAPITokenRequest]) (*connect.Response[v1.CreateAPITokenResponse], error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}

	msg := req.Msg
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("token name is required"))
	}

	plaintext, apiToken, err := s.authManager.GenerateAPIToken(ctx, user.ID, msg.Name, msg.ExpiresInDays)
	if err != nil {
		s.log.Error("Failed to create API token: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create API token"))
	}

	return connect.NewResponse(&v1.CreateAPITokenResponse{
		PlaintextToken: plaintext,
		ApiToken:       dbAPITokenToProto(apiToken),
	}), nil
}

func (s *AuthService) ListAPITokens(ctx context.Context, req *connect.Request[v1.ListAPITokensRequest]) (*connect.Response[v1.ListAPITokensResponse], error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}

	tokens, err := s.store.ListAPITokensByUser(ctx, user.ID)
	if err != nil {
		s.log.Error("Failed to list API tokens: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list API tokens"))
	}

	protoTokens := make([]*v1.ApiToken, 0, len(tokens))
	for _, t := range tokens {
		protoTokens = append(protoTokens, dbAPITokenToProto(&t))
	}

	return connect.NewResponse(&v1.ListAPITokensResponse{
		ApiTokens: protoTokens,
	}), nil
}

func (s *AuthService) DeleteAPIToken(ctx context.Context, req *connect.Request[v1.DeleteAPITokenRequest]) (*connect.Response[v1.DeleteAPITokenResponse], error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}

	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("token ID is required"))
	}

	if err := s.store.DeleteAPIToken(ctx, req.Msg.Id, user.ID); err != nil {
		s.log.Error("Failed to delete API token: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, errors.New("API token not found"))
	}

	return connect.NewResponse(&v1.DeleteAPITokenResponse{}), nil
}

func (s *AuthService) UseRecoveryKey(ctx context.Context, req *connect.Request[v1.UseRecoveryKeyRequest]) (*connect.Response[v1.UseRecoveryKeyResponse], error) {
	if req.Msg.RecoveryKey == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("recovery key is required"))
	}

	if err := s.authManager.UseRecoveryKey(ctx, req.Msg.RecoveryKey); err != nil {
		if errors.Is(err, auth.ErrInvalidRecoveryKey) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("invalid recovery key"))
		}
		s.log.Error("Recovery key reset failed: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("recovery reset failed"))
	}

	return connect.NewResponse(&v1.UseRecoveryKeyResponse{
		Message: "panel reset to first-user setup",
	}), nil
}

func dbAPITokenToProto(t *storage.APIToken) *v1.ApiToken {
	pt := &v1.ApiToken{
		Id:        t.ID,
		Name:      t.Name,
		CreatedAt: timestamppb.New(t.CreatedAt),
	}
	if t.ExpiresAt != nil {
		pt.ExpiresAt = timestamppb.New(*t.ExpiresAt)
	}
	if t.LastUsedAt != nil {
		pt.LastUsedAt = timestamppb.New(*t.LastUsedAt)
	}
	return pt
}

func dbInviteToProto(invite *storage.RegistrationInvite) *v1.RegistrationInvite {
	pi := &v1.RegistrationInvite{
		Id:          invite.ID,
		Code:        invite.Code,
		Description: invite.Description,
		Roles:       invite.Roles,
		HasPin:      invite.PinHash != "",
		MaxUses:     int32(invite.MaxUses),
		UseCount:    int32(invite.UseCount),
		CreatedBy:   invite.CreatedBy,
		CreatedAt:   timestamppb.New(invite.CreatedAt),
	}
	if invite.ExpiresAt != nil {
		pi.ExpiresAt = timestamppb.New(*invite.ExpiresAt)
	}
	return pi
}
