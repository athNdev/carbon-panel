// Package provision drives Terraform-managed node lifecycles.
//
// Status model (persisted on db.Provision.Status, transitions only forward
// except via failed):
//
//	pending -> planned -> applying -> applied -> destroying -> destroyed
//	pending|planned|applying -> failed
//
// Deny by default: plan/apply refuse unknown providers, regions, node types,
// missing credentials, missing plans and stale plan hashes. No partial apply
// is ever attempted: Apply checks the gate before invoking Terraform.
package provision

import "errors"

var (
	// ErrUnknownProvider is returned for a provider not in the registry.
	ErrUnknownProvider = errors.New("provision: unknown provider")
	// ErrUnknownRegion is returned for a region the provider does not declare.
	ErrUnknownRegion = errors.New("provision: unknown region")
	// ErrUnknownNodeType is returned for a node type outside nano|small|medium|large|xlarge|custom.
	ErrUnknownNodeType = errors.New("provision: unknown node type")
	// ErrMissingCredentials names the absent provider credential keys.
	ErrMissingCredentials = errors.New("provision: missing provider credentials")
	// ErrPlanRequired is returned when applying without a stored plan.
	ErrPlanRequired = errors.New("provision: apply requires a plan first")
	// ErrPlanChanged is returned when the stored plan hash moved since planning.
	ErrPlanChanged = errors.New("provision: plan changed since apply was requested")
	// ErrInvalidWorkspace is returned for workspace names that escape the root.
	ErrInvalidWorkspace = errors.New("provision: invalid workspace")
	// ErrNotFound is returned when a provision row is absent in the caller's org.
	ErrNotFound = errors.New("provision: not found")
	// ErrNoOrg is returned when the caller context selects no tenant.
	ErrNoOrg = errors.New("provision: no org in context")
)

// MissingCredentialsError carries the absent key names.
type MissingCredentialsError struct {
	Provider string
	Missing  []string
}

func (e *MissingCredentialsError) Error() string {
	return ErrMissingCredentials.Error() + ": provider " + e.Provider
}

// Unwrap lets errors.Is(err, ErrMissingCredentials) succeed.
func (e *MissingCredentialsError) Unwrap() error { return ErrMissingCredentials }
