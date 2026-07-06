package sync

var (
	_ Hub       = (*PostgresHub)(nil)
	_ HubServer = (*PostgresHub)(nil)
)
