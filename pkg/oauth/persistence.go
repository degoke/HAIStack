package oauth

// UsesInMemoryStores reports whether cfg relies on in-process stores for auth codes,
// refresh tokens, launch tokens, or revocation. This is the default when
// store.ApplySQLiteStores has not been called; production hosts should wire SQLite.
func UsesInMemoryStores(cfg Config) bool {
	if cfg.CodeStore == nil {
		return true
	}
	if _, ok := cfg.CodeStore.(*CodeStore); ok {
		return true
	}
	if cfg.RefreshStore == nil {
		return true
	}
	if _, ok := cfg.RefreshStore.(*MemoryRefreshStore); ok {
		return true
	}
	if cfg.LaunchStore == nil {
		return true
	}
	if _, ok := cfg.LaunchStore.(*MemoryLaunchStore); ok {
		return true
	}
	if cfg.RevocationStore == nil {
		return true
	}
	if _, ok := cfg.RevocationStore.(*MemoryRevocationStore); ok {
		return true
	}
	return false
}
