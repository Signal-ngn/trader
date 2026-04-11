package middleware

import (
	"context"

	"github.com/google/uuid"
)

// StaticUserRepo is a single-entry UserRepository that validates an incoming
// API key against a preconfigured key and returns a fixed tenant ID.
// Suitable for single-tenant deployments where the trader instance serves
// one user identified by SN_API_KEY.
type StaticUserRepo struct {
	apiKey   uuid.UUID
	tenantID uuid.UUID
}

// NewStaticUserRepo creates a StaticUserRepo. If apiKey is uuid.Nil the repo
// will never match any key (equivalent to passing nil as the UserRepository).
func NewStaticUserRepo(apiKey, tenantID uuid.UUID) *StaticUserRepo {
	return &StaticUserRepo{apiKey: apiKey, tenantID: tenantID}
}

// GetByAPIKey returns the configured tenant if key matches, nil otherwise.
func (r *StaticUserRepo) GetByAPIKey(_ context.Context, apiKey uuid.UUID) (*AuthUser, error) {
	if apiKey == r.apiKey {
		return &AuthUser{TenantID: r.tenantID}, nil
	}
	return nil, nil
}
