package auth_token

import "context"

// MintForTest drives one mint. Token caches, so a test that needs a second mint on
// the same Service cannot get there through the public API.
func (m *Service) MintForTest(ctx context.Context) error {
	_, err := m.mint(ctx)
	return err
}
