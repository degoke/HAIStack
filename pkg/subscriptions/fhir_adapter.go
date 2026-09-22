package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/search"
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
		Event:        TriggerEventChange,
		Criteria:     criteria,
	}
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		values, err := url.ParseQuery(parts[1])
		if err != nil {
			return Trigger{}, fmt.Errorf("%w: invalid criteria query: %v", ErrUnsupportedFHIR, err)
		}
		parsed, err := search.ParseQuery(resourceType, values)
		if err != nil {
			return Trigger{}, fmt.Errorf("%w: parse criteria: %v", ErrUnsupportedFHIR, err)
		}
		if err := rejectUnsupportedCriteria(parsed); err != nil {
			return Trigger{}, err
		}
		trigger.FilterParams = parsed.Params
	}
	if filter := fhirPathFromExtensions(extensions); filter != "" {
		trigger.FilterFHIRPath = filter
	}
	return trigger, nil
}

func rejectUnsupportedCriteria(parsed *search.Query) error {
	if parsed == nil {
		return nil
	}
	if len(parsed.Includes) > 0 || len(parsed.RevIncludes) > 0 || len(parsed.Chains) > 0 {
		return fmt.Errorf("%w: criteria includes, revincludes, and chained parameters are not supported", ErrUnsupportedFHIR)
	}
	if parsed.FullText != "" {
		return fmt.Errorf("%w: full-text criteria are not supported", ErrUnsupportedFHIR)
	}
	for _, clause := range parsed.Params {
		if strings.TrimSpace(clause.Modifier) != "" {
			return fmt.Errorf("%w: criteria modifiers are not supported", ErrUnsupportedFHIR)
		}
		for _, value := range clause.Values {
			if err := rejectUnsupportedPrefix(value); err != nil {
				return err
			}
		}
	}
	return nil
}

// fhirSearchPrefixes are FHIR search comparator prefixes (date/number/quantity).
// ParseQuery leaves them glued to ValueClause.Raw with Operator OpEqual, so
// registration has to detect them from the raw value.
var fhirSearchPrefixes = []string{"eq", "ne", "gt", "lt", "ge", "le", "sa", "eb", "ap"}

func rejectUnsupportedPrefix(value search.ValueClause) error {
	if strings.TrimSpace(value.Prefix) != "" {
		return fmt.Errorf("%w: criteria prefixes are not supported", ErrUnsupportedFHIR)
	}
	if value.Operator != "" && value.Operator != search.OpEqual {
		return fmt.Errorf("%w: criteria operators other than eq are not supported", ErrUnsupportedFHIR)
	}
	if prefix := fhirSearchPrefix(value.Raw); prefix != "" {
		return fmt.Errorf("%w: criteria prefixes are not supported", ErrUnsupportedFHIR)
	}
	return nil
}

func fhirSearchPrefix(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, prefix := range fhirSearchPrefixes {
		if len(raw) <= len(prefix) || !strings.HasPrefix(raw, prefix) {
			continue
		}
		if fhirPrefixedValueRemainder(raw[len(prefix):]) {
			return prefix
		}
	}
	return ""
}

func fhirPrefixedValueRemainder(rest string) bool {
	if rest == "" {
		return false
	}
	switch rest[0] {
	case '+', '-':
		return len(rest) > 1 && rest[1] >= '0' && rest[1] <= '9'
	default:
		return rest[0] >= '0' && rest[0] <= '9'
	}
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
