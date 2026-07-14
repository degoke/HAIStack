package smart

// ValidateToken parses and validates a compact JWT using an optional verifier.
// When verifier is nil, only structural claim validation runs.
func ValidateToken(token string, verifier SignatureVerifier, opts TokenValidateOptions) (TokenClaims, error) {
	return NewTokenValidator(verifier).ValidateToken(token, opts)
}

// ValidateBackendAssertion validates a SMART backend-service client assertion
// against the given BackendServiceAuth registry.
func ValidateBackendAssertion(b *BackendServiceAuth, assertion string, opts TokenValidateOptions) (TokenClaims, BackendClient, error) {
	if b == nil {
		return TokenClaims{}, BackendClient{}, ErrInvalidConfig
	}
	return b.ValidateBackendAssertion(assertion, opts)
}
