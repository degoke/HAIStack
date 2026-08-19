package validate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/proto"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type countingCodec struct {
	inner *proto.GoogleR4Codec
	calls int
	mu    sync.Mutex
}

func (c *countingCodec) ParseJSONToProto(resourceType string, data []byte) (any, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.inner.ParseJSONToProto(resourceType, data)
}

func (c *countingCodec) ParseJSONToEnvelope(resourceType string, data []byte) (*types.ResourceEnvelope, error) {
	return c.inner.ParseJSONToEnvelope(resourceType, data)
}

func (c *countingCodec) ProtoToJSON(resourceType string, protoVal any) ([]byte, error) {
	return c.inner.ProtoToJSON(resourceType, protoVal)
}

func (c *countingCodec) ProtoToEnvelope(resourceType string, protoVal any) (*types.ResourceEnvelope, error) {
	return c.inner.ProtoToEnvelope(resourceType, protoVal)
}

func (c *countingCodec) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestValidateReusesEnvelopeProto(t *testing.T) {
	codec := proto.NewGoogleR4Codec()
	data := []byte(`{"resourceType":"Patient","id":"pat-1","name":[{"family":"Doe"}]}`)
	pb, err := codec.ParseJSONToProto("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	env, err := codec.ProtoToEnvelope("Patient", pb)
	if err != nil {
		t.Fatalf("ProtoToEnvelope: %v", err)
	}

	counter := &countingCodec{inner: codec}
	eng, err := NewEngine(Config{ProtoCodec: counter})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	result, err := eng.Validate(context.Background(), env, ValidateOptions{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %+v", result.Issues)
	}
	if counter.callCount() != 0 {
		t.Fatalf("ParseJSONToProto calls = %d, want 0 (reuse envelope proto)", counter.callCount())
	}
}

func TestValidateRejectsJSONProtoDrift(t *testing.T) {
	codec := proto.NewGoogleR4Codec()
	data := []byte(`{"resourceType":"Patient","id":"pat-1","name":[{"family":"Doe"}]}`)
	pb, err := codec.ParseJSONToProto("Patient", data)
	if err != nil {
		t.Fatalf("ParseJSONToProto: %v", err)
	}
	env, err := codec.ProtoToEnvelope("Patient", pb)
	if err != nil {
		t.Fatalf("ProtoToEnvelope: %v", err)
	}
	env.JSON = []byte(`{"resourceType":"Patient","id":"pat-1","active":"not-a-boolean"}`)

	eng, err := NewEngine(Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	result, err := eng.Validate(context.Background(), env, ValidateOptions{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid result for JSON/proto drift")
	}
	found := false
	for _, iss := range result.Issues {
		if iss.Code == "structural" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected structural issue, got %+v", result.Issues)
	}
}

func TestValidateDetectsAttachedProtoTypeMismatch(t *testing.T) {
	codec := proto.NewGoogleR4Codec()
	patientData := []byte(`{"resourceType":"Patient","id":"pat-1","name":[{"family":"Doe"}]}`)
	obsData := []byte(`{"resourceType":"Observation","id":"obs-1","status":"final","code":{"text":"hr"}}`)

	patientPB, err := codec.ParseJSONToProto("Patient", patientData)
	if err != nil {
		t.Fatalf("ParseJSONToProto patient: %v", err)
	}
	obsPB, err := codec.ParseJSONToProto("Observation", obsData)
	if err != nil {
		t.Fatalf("ParseJSONToProto observation: %v", err)
	}
	env, err := codec.ProtoToEnvelope("Patient", patientPB)
	if err != nil {
		t.Fatalf("ProtoToEnvelope patient: %v", err)
	}
	env.Proto = obsPB

	eng, err := NewEngine(Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	result, err := eng.Validate(context.Background(), env, ValidateOptions{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid result for JSON/proto type mismatch")
	}
	found := false
	for _, iss := range result.Issues {
		if iss.Code == "structural" && strings.Contains(iss.Diagnostics, "mismatch") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected structural mismatch issue, got %+v", result.Issues)
	}
}

func TestValidateContextCancellation(t *testing.T) {
	eng, err := NewEngine(Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	env := &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         []byte(`{"resourceType":"Patient","id":"pat-1"}`),
	}
	_, err = eng.Validate(ctx, env, ValidateOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestNewEngineInvalidConfig(t *testing.T) {
	_, err := NewEngine(Config{KnownResourceTypes: map[string]struct{}{}})
	if err == nil {
		t.Fatal("expected error for empty KnownResourceTypes")
	}

	_, err = NewEngine(Config{RequiredFields: map[string][]string{"Patient": {""}}})
	if err == nil {
		t.Fatal("expected error for empty required field name")
	}
}
