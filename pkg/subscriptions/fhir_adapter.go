package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
)

const (
	fhirPathExtensionURL = "http://haistack.dev/fhir/StructureDefinition/subscription-fhirpath-filter"
)

// FHIRSubscriptionInput carries a supported subset of FHIR Subscription fields.
type FHIRSubscriptionInput struct {
	ID       string
	Status   string
	Criteria string
	Channel  FHIRSubscriptionChannel
	Name     string
}

// FHIRSubscriptionChannel maps FHIR Subscription.channel.
type FHIRSubscriptionChannel struct {
	Type     string
	Endpoint string
	Payload  string
	Headers  []string
}

// RegisterFromFHIRSubscription adapts a supported FHIR Subscription shape into
// the internal trigger model and registers it.
func (m *Manager) RegisterFromFHIRSubscription(ctx context.Context, input FHIRSubscriptionInput, extensions []map[string]any) (SubscriptionRecord, error) {
	trigger, err := triggerFromFHIR(input.Criteria, extensions)
	if err != nil {
		return SubscriptionRecord{}, err
	}
	channel, err := channelFromFHIR(input.Channel, input.Channel.Payload)
	if err != nil {
		return SubscriptionRecord{}, err
	}
	name := input.Name
	if name == "" {
		name = input.ID
	}
	if name == "" {
		name = fmt.Sprintf("%s.%s", trigger.ResourceType, trigger.Event)
	}
	rec, err := m.Register(ctx, name, trigger, channel, defaultRetryPolicy())
	if err != nil {
		return SubscriptionRecord{}, err
	}
	if input.Status == "off" || input.Status == "error" {
		rec.Status = store.SubscriptionStatusDisabled
		stored, convErr := toStoreRecord(rec)
		if convErr != nil {
			return SubscriptionRecord{}, convErr
		}
		if err := m.Store.Update(ctx, stored); err != nil {
			return SubscriptionRecord{}, err
		}
	}
	return rec, nil
}

func triggerFromFHIR(criteria string, extensions []map[string]any) (Trigger, error) {
	criteria = strings.TrimSpace(criteria)
	if criteria == "" {
		return Trigger{}, fmt.Errorf("%w: criteria is required", ErrUnsupportedFHIR)
	}
	parts := strings.SplitN(criteria, "?", 2)
	resourceType := strings.TrimSpace(parts[0])
	if resourceType == "" {
		return Trigger{}, fmt.Errorf("%w: criteria must start with a resource type", ErrUnsupportedFHIR)
	}
	trigger := Trigger{
		ResourceType: resourceType,
		Event:        TriggerEventCreate,
	}
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		return Trigger{}, fmt.Errorf("%w: advanced search criteria are not supported yet", ErrUnsupportedFHIR)
	}
	if filter := fhirPathFromExtensions(extensions); filter != "" {
		trigger.FilterFHIRPath = filter
	}
	return trigger, nil
}

func channelFromFHIR(ch FHIRSubscriptionChannel, payload string) (Channel, error) {
	switch strings.ToLower(strings.TrimSpace(ch.Type)) {
	case "rest-hook":
		headers := map[string]string{}
		for _, h := range ch.Headers {
			k, v, ok := strings.Cut(h, ":")
			if !ok {
				return Channel{}, fmt.Errorf("%w: invalid header %q", ErrUnsupportedFHIR, h)
			}
			headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
		mode := PayloadModeResourceJSON
		if strings.TrimSpace(payload) == "" {
			mode = PayloadModeEventOnly
		}
		if ch.Endpoint == "" {
			return Channel{}, fmt.Errorf("%w: rest-hook endpoint is required", ErrUnsupportedFHIR)
		}
		return Channel{
			Type: ChannelTypeWebhook,
			Webhook: &WebhookConfig{
				URL:         ch.Endpoint,
				Method:      "POST",
				Headers:     headers,
				PayloadMode: mode,
			},
		}, nil
	case "websocket", "email", "sms", "message":
		return Channel{}, fmt.Errorf("%w: channel type %q is not supported yet", ErrUnsupportedFHIR, ch.Type)
	default:
		return Channel{}, fmt.Errorf("%w: unknown channel type %q", ErrUnsupportedFHIR, ch.Type)
	}
}

func fhirPathFromExtensions(extensions []map[string]any) string {
	for _, ext := range extensions {
		url, _ := ext["url"].(string)
		if url != fhirPathExtensionURL {
			continue
		}
		if v, ok := ext["valueString"].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ParseFHIRSubscriptionJSON decodes a FHIR Subscription resource JSON blob.
func ParseFHIRSubscriptionJSON(data []byte) (FHIRSubscriptionInput, []map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return FHIRSubscriptionInput{}, nil, err
	}
	rt, _ := raw["resourceType"].(string)
	if rt != "Subscription" {
		return FHIRSubscriptionInput{}, nil, fmt.Errorf("%w: expected Subscription resource", ErrUnsupportedFHIR)
	}
	input := FHIRSubscriptionInput{
		ID:       stringField(raw, "id"),
		Status:   stringField(raw, "status"),
		Criteria: stringField(raw, "criteria"),
		Name:     stringField(raw, "reason"),
	}
	if ch, ok := raw["channel"].(map[string]any); ok {
		input.Channel = FHIRSubscriptionChannel{
			Type:     stringField(ch, "type"),
			Endpoint: stringField(ch, "endpoint"),
			Payload:  stringField(ch, "payload"),
		}
		if headers, ok := ch["header"].([]any); ok {
			for _, h := range headers {
				if s, ok := h.(string); ok {
					input.Channel.Headers = append(input.Channel.Headers, s)
				}
			}
		}
	}
	var extensions []map[string]any
	if exts, ok := raw["extension"].([]any); ok {
		for _, item := range exts {
			if m, ok := item.(map[string]any); ok {
				extensions = append(extensions, m)
			}
		}
	}
	return input, extensions, nil
}

func stringField(obj map[string]any, key string) string {
	if v, ok := obj[key].(string); ok {
		return v
	}
	return ""
}
