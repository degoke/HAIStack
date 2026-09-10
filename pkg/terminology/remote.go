package terminology

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RemoteConfig configures a FHIR terminology server client (tx.fhir.org, Snowstorm, etc.).
type RemoteConfig struct {
	BaseURL string
	Client  *http.Client

	// CacheTTL caches remote lookup and expansion responses. Zero disables caching.
	CacheTTL time.Duration

	// MinInterval enforces a minimum delay between outbound requests.
	MinInterval time.Duration

	// FailureThreshold opens the circuit after this many consecutive failures.
	FailureThreshold int

	// CircuitOpenDuration keeps the circuit open after the threshold is reached.
	CircuitOpenDuration time.Duration
}

// RemoteProvider resolves terminology via FHIR R4 $lookup, $expand, and $validate-code.
type RemoteProvider struct {
	baseURL string
	client  *http.Client

	cacheTTL            time.Duration
	minInterval         time.Duration
	failureThreshold    int
	circuitOpenDuration time.Duration

	mu            sync.Mutex
	lastRequest   time.Time
	failures      int
	circuitOpenAt time.Time
	lookupCache   map[string]cacheEntry[*LookupResult]
	expandCache   map[string]cacheEntry[*Expansion]
}

type cacheEntry[T any] struct {
	value     T
	expiresAt time.Time
}

// NewRemoteProvider constructs a remote terminology provider.
func NewRemoteProvider(cfg RemoteConfig) (*RemoteProvider, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("remote terminology base URL is required")
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	threshold := cfg.FailureThreshold
	if threshold <= 0 {
		threshold = 5
	}
	openFor := cfg.CircuitOpenDuration
	if openFor <= 0 {
		openFor = 30 * time.Second
	}
	return &RemoteProvider{
		baseURL:             base,
		client:              client,
		cacheTTL:            cfg.CacheTTL,
		minInterval:         cfg.MinInterval,
		failureThreshold:    threshold,
		circuitOpenDuration: openFor,
	}, nil
}

func (p *RemoteProvider) Lookup(ctx context.Context, r LookupRequest) (*LookupResult, error) {
	if r.System == "" || r.Code == "" {
		return nil, fmt.Errorf("system and code are required")
	}
	key := r.System + "|" + r.Version + "|" + r.Code
	if v, ok := p.cachedLookup(key); ok {
		return v, nil
	}
	params, err := p.callParameters(ctx, "CodeSystem/$lookup", url.Values{
		"system":  {r.System},
		"code":    {r.Code},
		"version": optionalQuery(r.Version),
	})
	if err != nil {
		return nil, err
	}
	if params == nil {
		return &LookupResult{Found: false}, nil
	}
	found := parameterBool(params, "result")
	if !found {
		return &LookupResult{Found: false}, nil
	}
	result := &LookupResult{
		Found: true,
		Concept: Concept{
			System:  r.System,
			Version: firstNonEmpty(parameterString(params, "version"), r.Version),
			Code:    r.Code,
			Display: parameterString(params, "display"),
		},
	}
	p.storeLookup(key, result)
	return result, nil
}

func (p *RemoteProvider) Expand(ctx context.Context, r ExpandRequest) (*Expansion, error) {
	if r.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	key := r.URL + "|" + r.Version + fmt.Sprintf("|%d|%d", r.Offset, r.Count)
	if v, ok := p.cachedExpand(key); ok {
		return v, nil
	}
	q := url.Values{"url": {r.URL}}
	if r.Version != "" {
		q.Set("version", r.Version)
	}
	if r.Offset > 0 {
		q.Set("offset", fmt.Sprintf("%d", r.Offset))
	}
	if r.Count > 0 {
		q.Set("count", fmt.Sprintf("%d", r.Count))
	}
	body, err := p.callResource(ctx, "ValueSet/$expand", q)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	expansion, err := parseValueSetExpansion(body)
	if err != nil {
		return nil, err
	}
	if expansion == nil {
		return nil, nil
	}
	p.storeExpand(key, expansion)
	return expansion, nil
}

func (p *RemoteProvider) ValidateCode(ctx context.Context, r ValidateCodeRequest) (*ValidationResult, error) {
	if r.Coding.System == "" || r.Coding.Code == "" {
		return &ValidationResult{Status: Invalid, Message: "coding system and code are required"}, nil
	}
	op := "CodeSystem/$validate-code"
	q := url.Values{
		"code":    {r.Coding.Code},
		"system":  {r.Coding.System},
		"version": optionalQuery(firstNonEmpty(r.Coding.Version, r.Version)),
		"display": optionalQuery(r.Coding.Display),
	}
	if r.URL != "" {
		op = "ValueSet/$validate-code"
		q.Set("url", r.URL)
		if r.Version != "" {
			q.Set("valueSetVersion", r.Version)
		}
	}
	params, err := p.callParameters(ctx, op, q)
	if err != nil {
		if isUnavailable(err) {
			return &ValidationResult{Status: UnavailableProvider, Message: err.Error()}, nil
		}
		return nil, err
	}
	if params == nil {
		return &ValidationResult{Status: UnknownTerminology, Message: "terminology is not known"}, nil
	}
	if parameterBool(params, "result") {
		return &ValidationResult{Status: Valid, Message: parameterString(params, "message")}, nil
	}
	return &ValidationResult{Status: Invalid, Message: firstNonEmpty(parameterString(params, "message"), "code is not valid")}, nil
}

func (p *RemoteProvider) callParameters(ctx context.Context, operation string, query url.Values) (map[string]any, error) {
	body, err := p.callResource(ctx, operation, query)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	var params map[string]any
	if err := json.Unmarshal(body, &params); err != nil {
		return nil, fmt.Errorf("decode remote terminology response: %w", err)
	}
	if rt, _ := params["resourceType"].(string); rt != "Parameters" {
		return nil, fmt.Errorf("unexpected remote terminology response type %q", rt)
	}
	return params, nil
}

func (p *RemoteProvider) callResource(ctx context.Context, operation string, query url.Values) ([]byte, error) {
	if err := p.checkCircuit(); err != nil {
		return nil, err
	}
	if err := p.throttle(); err != nil {
		return nil, err
	}
	endpoint := p.baseURL + "/" + strings.TrimPrefix(operation, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = encodeQuery(query)
	resp, err := p.client.Do(req)
	if err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("remote terminology request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		p.recordFailure()
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		p.recordSuccess()
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.recordFailure()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, unavailableError(fmt.Sprintf("remote terminology HTTP %d", resp.StatusCode))
		}
		return nil, fmt.Errorf("remote terminology HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	p.recordSuccess()
	return body, nil
}

func parseValueSetExpansion(body []byte) (*Expansion, error) {
	var vs map[string]any
	if err := json.Unmarshal(body, &vs); err != nil {
		return nil, err
	}
	exp, _ := vs["expansion"].(map[string]any)
	if exp == nil {
		return nil, nil
	}
	out := &Expansion{
		URL:     stringValue(vs["url"]),
		Version: stringValue(vs["version"]),
	}
	if total, ok := exp["total"].(float64); ok {
		out.Total = int(total)
	}
	contains, _ := exp["contains"].([]any)
	for _, item := range contains {
		m, _ := item.(map[string]any)
		out.Contains = append(out.Contains, Coding{
			System:  stringValue(m["system"]),
			Version: stringValue(m["version"]),
			Code:    stringValue(m["code"]),
			Display: stringValue(m["display"]),
		})
	}
	if out.Total == 0 {
		out.Total = len(out.Contains)
	}
	return out, nil
}

func parameterString(params map[string]any, name string) string {
	for _, p := range parameterList(params) {
		n, _ := p["name"].(string)
		if n != name {
			continue
		}
		if v, ok := p["valueString"].(string); ok {
			return v
		}
		if v, ok := p["valueCode"].(string); ok {
			return v
		}
		if v, ok := p["valueUri"].(string); ok {
			return v
		}
	}
	return ""
}

func parameterBool(params map[string]any, name string) bool {
	for _, p := range parameterList(params) {
		n, _ := p["name"].(string)
		if n != name {
			continue
		}
		if v, ok := p["valueBoolean"].(bool); ok {
			return v
		}
	}
	return false
}

func parameterList(params map[string]any) []map[string]any {
	raw, _ := params["parameter"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func optionalQuery(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return []string{v}
}

func encodeQuery(values url.Values) string {
	filtered := url.Values{}
	for key, vals := range values {
		for _, val := range vals {
			if strings.TrimSpace(val) != "" {
				filtered.Add(key, val)
			}
		}
	}
	return filtered.Encode()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (p *RemoteProvider) checkCircuit() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.circuitOpenAt.IsZero() || time.Now().After(p.circuitOpenAt) {
		return nil
	}
	return unavailableError("remote terminology circuit is open")
}

func (p *RemoteProvider) throttle() error {
	if p.minInterval <= 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.lastRequest.IsZero() {
		wait := p.minInterval - time.Since(p.lastRequest)
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	p.lastRequest = time.Now()
	return nil
}

func (p *RemoteProvider) recordFailure() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures++
	if p.failures >= p.failureThreshold {
		p.circuitOpenAt = time.Now().Add(p.circuitOpenDuration)
		p.failures = 0
	}
}

func (p *RemoteProvider) recordSuccess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures = 0
	p.circuitOpenAt = time.Time{}
}

func unavailableError(message string) error {
	return &remoteUnavailableError{message: message}
}

type remoteUnavailableError struct{ message string }

func (e *remoteUnavailableError) Error() string { return e.message }

func isUnavailable(err error) bool {
	_, ok := err.(*remoteUnavailableError)
	return ok
}

func (p *RemoteProvider) cachedLookup(key string) (*LookupResult, bool) {
	if p.cacheTTL <= 0 {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lookupCache == nil {
		return nil, false
	}
	entry, ok := p.lookupCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

func (p *RemoteProvider) storeLookup(key string, value *LookupResult) {
	if p.cacheTTL <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lookupCache == nil {
		p.lookupCache = map[string]cacheEntry[*LookupResult]{}
	}
	p.lookupCache[key] = cacheEntry[*LookupResult]{value: value, expiresAt: time.Now().Add(p.cacheTTL)}
}

func (p *RemoteProvider) cachedExpand(key string) (*Expansion, bool) {
	if p.cacheTTL <= 0 {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.expandCache == nil {
		return nil, false
	}
	entry, ok := p.expandCache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

func (p *RemoteProvider) storeExpand(key string, value *Expansion) {
	if p.cacheTTL <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.expandCache == nil {
		p.expandCache = map[string]cacheEntry[*Expansion]{}
	}
	p.expandCache[key] = cacheEntry[*Expansion]{value: value, expiresAt: time.Now().Add(p.cacheTTL)}
}
