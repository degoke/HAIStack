package subscriptions

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// MatchContext carries runtime state for one event evaluation.
type MatchContext struct {
	Event    store.ResourceEvent
	Current  *types.ResourceEnvelope
	Previous *types.ResourceEnvelope
}

// Matcher evaluates subscription triggers against resource events.
type Matcher struct {
	Engine fhirpath.Engine
}

// Matches reports whether trigger conditions are satisfied for one event.
func (m *Matcher) Matches(ctx context.Context, trigger Trigger, mc MatchContext) (bool, error) {
	if m == nil {
		return false, ErrNilEngine
	}
	if !eventActionMatches(trigger.Event, mc.Event.Action) {
		return false, nil
	}
	if trigger.ResourceType != mc.Event.ResourceType {
		return false, nil
	}
	if trigger.Event == TriggerEventUpdate && len(trigger.ChangedFields) > 0 {
		changed, err := changedTopLevelFields(mc.Previous, mc.Current)
		if err != nil {
			return false, err
		}
		if !intersects(trigger.ChangedFields, changed) {
			return false, nil
		}
	}
	if trigger.FilterFHIRPath != "" {
		resource := mc.Current
		if resource == nil && mc.Previous != nil {
			resource = mc.Previous
		}
		if resource == nil {
			return false, nil
		}
		ok, err := m.Engine.EvalBool(ctx, trigger.FilterFHIRPath, resource)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func changedTopLevelFields(previous, current *types.ResourceEnvelope) ([]string, error) {
	if previous == nil || current == nil {
		return nil, nil
	}
	var prevObj, currObj map[string]any
	if len(previous.JSON) > 0 {
		if err := json.Unmarshal(previous.JSON, &prevObj); err != nil {
			return nil, err
		}
	}
	if len(current.JSON) > 0 {
		if err := json.Unmarshal(current.JSON, &currObj); err != nil {
			return nil, err
		}
	}
	keys := make(map[string]struct{})
	for k := range prevObj {
		if !ignoredTopLevel(k) {
			keys[k] = struct{}{}
		}
	}
	for k := range currObj {
		if !ignoredTopLevel(k) {
			keys[k] = struct{}{}
		}
	}
	var changed []string
	for k := range keys {
		var prevVal, currVal any
		if prevObj != nil {
			prevVal = prevObj[k]
		}
		if currObj != nil {
			currVal = currObj[k]
		}
		if !reflect.DeepEqual(prevVal, currVal) {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

func intersects(want, got []string) bool {
	if len(want) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(got))
	for _, field := range got {
		set[strings.ToLower(field)] = struct{}{}
	}
	for _, field := range want {
		if _, ok := set[strings.ToLower(field)]; ok {
			return true
		}
	}
	return false
}

func ignoredTopLevel(k string) bool {
	return k == "id" || k == "meta" || k == "resourceType"
}
