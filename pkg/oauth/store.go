package oauth

// Deprecated: use AuthorizationStore via Config.AuthorizationStore.
// TokenStore remains as a thin alias for tests migrating to AuthorizationStore.
type TokenStore = MemoryAuthorizationStore

// NewTokenStore constructs an in-memory authorization store.
func NewTokenStore() *MemoryAuthorizationStore {
	return NewMemoryAuthorizationStore()
}
