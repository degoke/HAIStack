package binary

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// S3Config configures an S3-compatible object store.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	UsePathStyle    bool
	HTTPClient      *http.Client
}

// S3BlobStore stores blob bytes in an S3-compatible object storage backend.
type S3BlobStore struct {
	endpoint     *url.URL
	region       string
	bucket       string
	accessKeyID  string
	secretKey    string
	sessionToken string
	usePathStyle bool
	client       *http.Client
}

// NewS3BlobStore creates an S3-compatible object storage adapter.
func NewS3BlobStore(cfg S3Config) (*S3BlobStore, error) {
	if cfg.Endpoint == "" || cfg.Region == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("%w: endpoint, region, bucket, access key, and secret key are required", ErrInvalidArgument)
	}
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &S3BlobStore{
		endpoint:     endpoint,
		region:       cfg.Region,
		bucket:       cfg.Bucket,
		accessKeyID:  cfg.AccessKeyID,
		secretKey:    cfg.SecretAccessKey,
		sessionToken: cfg.SessionToken,
		usePathStyle: cfg.UsePathStyle,
		client:       client,
	}, nil
}

// Put stores blob bytes in object storage.
func (s *S3BlobStore) Put(ctx context.Context, blobID string, data []byte, contentType string) (*BlobDescriptor, error) {
	return s.PutWithOptions(ctx, blobID, data, BlobWriteOptions{ContentType: contentType})
}

// PutWithOptions stores blob bytes with retention-aware headers.
func (s *S3BlobStore) PutWithOptions(ctx context.Context, blobID string, data []byte, opts BlobWriteOptions) (*BlobDescriptor, error) {
	if blobID == "" {
		return nil, fmt.Errorf("%w: blobID is required", ErrInvalidArgument)
	}
	headers := map[string]string{}
	if opts.ContentType != "" {
		headers["content-type"] = opts.ContentType
	}
	if opts.Retention != nil && opts.Retention.Mode != RetentionNone {
		headers["x-amz-object-lock-mode"] = strings.ToUpper(string(opts.Retention.Mode))
		headers["x-amz-object-lock-retain-until-date"] = opts.Retention.RetainUntil.UTC().Format(time.RFC3339)
	}
	signed, err := s.presign(ctx, http.MethodPut, blobID, 15*time.Minute, headers)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, signed.URL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	for k, v := range signed.Headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("s3 put blob: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	hash := HashSHA256(data)
	return &BlobDescriptor{
		BlobID:      blobID,
		SHA256:      hash,
		Size:        int64(len(data)),
		ContentType: opts.ContentType,
		Backend:     BackendS3,
		Pointer: StoragePointer{
			Backend: BackendS3,
			Ref:     s.objectRef(blobID),
		},
		Retention: opts.Retention,
	}, nil
}

// Get reads blob bytes from object storage.
func (s *S3BlobStore) Get(ctx context.Context, blobID string) ([]byte, *BlobDescriptor, error) {
	signed, err := s.SignedGetURL(ctx, blobID, 15*time.Minute)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signed.URL, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, nil, fmt.Errorf("s3 get blob: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	desc := &BlobDescriptor{
		BlobID:      blobID,
		SHA256:      HashSHA256(data),
		Size:        int64(len(data)),
		Backend:     BackendS3,
		Pointer:     StoragePointer{Backend: BackendS3, Ref: s.objectRef(blobID)},
		ContentType: resp.Header.Get("Content-Type"),
	}
	return data, desc, nil
}

// Head returns object metadata.
func (s *S3BlobStore) Head(ctx context.Context, blobID string) (*BlobDescriptor, error) {
	signed, err := s.presign(ctx, http.MethodHead, blobID, 15*time.Minute, nil)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, signed.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("s3 head blob: status %d", resp.StatusCode)
	}
	return &BlobDescriptor{
		BlobID:      blobID,
		Size:        resp.ContentLength,
		ContentType: resp.Header.Get("Content-Type"),
		Backend:     BackendS3,
		Pointer:     StoragePointer{Backend: BackendS3, Ref: s.objectRef(blobID)},
	}, nil
}

// Delete removes an object.
func (s *S3BlobStore) Delete(ctx context.Context, blobID string) error {
	signed, err := s.presign(ctx, http.MethodDelete, blobID, 15*time.Minute, nil)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, signed.URL, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("s3 delete blob: status %d", resp.StatusCode)
	}
	return nil
}

// SignedGetURL creates a direct read URL.
func (s *S3BlobStore) SignedGetURL(ctx context.Context, blobID string, ttl time.Duration) (*SignedAccessURL, error) {
	return s.presign(ctx, http.MethodGet, blobID, ttl, nil)
}

// SignedPutURL creates a direct write URL.
func (s *S3BlobStore) SignedPutURL(ctx context.Context, blobID string, ttl time.Duration, contentType string) (*SignedAccessURL, error) {
	headers := map[string]string{}
	if contentType != "" {
		headers["content-type"] = contentType
	}
	return s.presign(ctx, http.MethodPut, blobID, ttl, headers)
}

func (s *S3BlobStore) presign(ctx context.Context, method, blobID string, ttl time.Duration, headers map[string]string) (*SignedAccessURL, error) {
	_ = ctx
	if blobID == "" {
		return nil, fmt.Errorf("%w: blobID is required", ErrInvalidArgument)
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	scope := dateStamp + "/" + s.region + "/s3/aws4_request"

	u := s.objectURL(blobID)
	query := u.Query()
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", s.accessKeyID+"/"+scope)
	query.Set("X-Amz-Date", amzDate)
	query.Set("X-Amz-Expires", fmt.Sprintf("%d", int(ttl.Seconds())))
	canonicalHeaders, signedHeaders := s.canonicalHeaders(headers)
	query.Set("X-Amz-SignedHeaders", signedHeaders)
	if s.sessionToken != "" {
		query.Set("X-Amz-Security-Token", s.sessionToken)
	}
	u.RawQuery = canonicalQuery(query)

	canonicalRequest := strings.Join([]string{
		method,
		u.EscapedPath(),
		u.RawQuery,
		canonicalHeaders,
		signedHeaders,
		"UNSIGNED-PAYLOAD",
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signingKey := deriveSigningKey(s.secretKey, dateStamp, s.region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	query.Set("X-Amz-Signature", signature)
	u.RawQuery = canonicalQuery(query)

	outHeaders := map[string]string{}
	for k, v := range headers {
		outHeaders[http.CanonicalHeaderKey(k)] = v
	}
	return &SignedAccessURL{
		Method:    method,
		URL:       u.String(),
		Headers:   outHeaders,
		ExpiresAt: now.Add(ttl),
	}, nil
}

func (s *S3BlobStore) objectURL(blobID string) *url.URL {
	u := *s.endpoint
	if s.usePathStyle {
		u.Path = path.Join(u.Path, s.bucket, blobID)
		return &u
	}
	u.Host = s.bucket + "." + u.Host
	u.Path = path.Join(u.Path, blobID)
	return &u
}

func (s *S3BlobStore) objectRef(blobID string) string {
	return "s3://" + s.bucket + "/" + blobID
}

func (s *S3BlobStore) canonicalHeaders(headers map[string]string) (string, string) {
	values := map[string]string{
		"host": s.objectURL("").Host,
	}
	for k, v := range headers {
		values[strings.ToLower(k)] = strings.TrimSpace(v)
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(":")
		b.WriteString(values[k])
		b.WriteString("\n")
	}
	return b.String(), strings.Join(keys, ";")
}

func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string(nil), values[k]...)
		sort.Strings(vals)
		escapedKey := url.QueryEscape(k)
		escapedKey = strings.ReplaceAll(escapedKey, "+", "%20")
		for _, v := range vals {
			escapedVal := url.QueryEscape(v)
			escapedVal = strings.ReplaceAll(escapedVal, "+", "%20")
			parts = append(parts, escapedKey+"="+escapedVal)
		}
	}
	return strings.Join(parts, "&")
}

func deriveSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(data))
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
