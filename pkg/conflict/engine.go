package conflict

import (
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Classification is the conflict bucket assigned to a detect result.
type Classification string

const (
	ClassificationNoConflict             Classification = "no_conflict"
	ClassificationStaleBaseOnly          Classification = "stale_base_only"
	ClassificationSameResourceNonOverlap Classification = "same_resource_non_overlapping_update"
	ClassificationSameResourceOverlap    Classification = "same_resource_overlapping_update"
	ClassificationAppendOnlyCompatible   Classification = "append_only_compatible"
	ClassificationClinicallySensitive    Classification = "clinically_sensitive_conflict"
	ClassificationUnsupportedMerge       Classification = "unsupported_merge_shape"
)

// RiskLevel is the clinical review tier for a conflict.
type RiskLevel string

const (
	RiskLevelSafe    RiskLevel = "safe"
	RiskLevelReview  RiskLevel = "review"
	RiskLevelBlocked RiskLevel = "blocked"
)

// LocalEvent is the mutation shape the conflict engine accepts. It mirrors
// sync.LocalEvent so the package can be used without importing sync.
type LocalEvent struct {
	EventID          string
	ResourceType     string
	ResourceID       string
	Operation        string
	BaseCloudVersion string
	LocalVersion     string
	ChangedPaths     []string
	Patch            []byte
	ResourceAfter    *types.ResourceEnvelope
	ResourceHash     string
}

// Result captures the classification and mergeability of a conflict.
type Result struct {
	Classification   Classification
	Risk             RiskLevel
	AutoMergeable    bool
	LocalChanges     []PathChange
	RemoteChanges    []PathChange
	OverlappingPaths []string
	ReviewReason     string
}

// ReviewMetadata is the UI-facing summary for non-auto-mergeable conflicts.
type ReviewMetadata struct {
	Reason            string
	LocalPathSummary  []string
	RemotePathSummary []string
	OverlapPaths      []string
	UILabels          map[string]string
}

// MergeResult is the output of a successful or blocked merge attempt.
type MergeResult struct {
	Result        Result
	AutoMergeable bool
	Merged        *types.ResourceEnvelope
	Patch         []byte
	Resolution    Classification
	Review        ReviewMetadata
}

// Config wires optional policy and clock into the engine.
type Config struct {
	Registry *PolicyRegistry
	Clock    func() time.Time
}

// Engine is the high-level conflict detection and merge entrypoint.
type Engine struct {
	cfg Config
}

// NewDefaultEngine builds an engine with the built-in default policy.
func NewDefaultEngine() *Engine {
	return NewEngine(Config{Registry: DefaultPolicyRegistry()})
}

// NewEngine builds an engine from config, filling in defaults.
func NewEngine(cfg Config) *Engine {
	if cfg.Registry == nil {
		cfg.Registry = DefaultPolicyRegistry()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &Engine{cfg: cfg}
}

// Detect evaluates a local sync event against the base and current resource states.
func (e *Engine) Detect(local LocalEvent, base, current *types.ResourceEnvelope) Result {
	if e == nil {
		return Result{Classification: ClassificationUnsupportedMerge, Risk: RiskLevelBlocked, AutoMergeable: false}
	}
	if !validLocalEvent(local) {
		return unsupportedResult("invalid local event")
	}
	if local.Operation == "resource.deleted" || local.Operation == "resource.created" {
		return unsupportedResult("unsupported operation: " + local.Operation)
	}
	if base == nil || current == nil || base.JSON == nil || current.JSON == nil {
		return unsupportedResult("missing base or current resource")
	}
	if local.ResourceAfter == nil || local.ResourceAfter.JSON == nil {
		return unsupportedResult("missing local resource payload")
	}

	localChanges, err := diffJSON(base.JSON, local.ResourceAfter.JSON)
	if err != nil {
		return unsupportedResult("diff local change: " + err.Error())
	}
	remoteChanges, err := diffJSON(base.JSON, current.JSON)
	if err != nil {
		return unsupportedResult("diff remote change: " + err.Error())
	}

	overlaps := computeOverlaps(localChanges, remoteChanges, local.ResourceType)
	risk := e.riskFor(local.ResourceType, localChanges, remoteChanges)
	classification := classify(local.ResourceType, localChanges, remoteChanges, overlaps, e.cfg.Registry)
	auto := e.canAutoMerge(local.ResourceType, localChanges, remoteChanges, overlaps, risk)

	reason := ""
	if !auto {
		reason = reviewReason(classification, risk, overlaps)
	}

	return Result{
		Classification:   classification,
		Risk:             risk,
		AutoMergeable:    auto,
		LocalChanges:     localChanges,
		RemoteChanges:    remoteChanges,
		OverlappingPaths: overlaps,
		ReviewReason:     reason,
	}
}

// CanAutoMerge reports whether a detected conflict can be merged automatically.
func (e *Engine) CanAutoMerge(r Result) bool {
	return r.AutoMergeable
}

// Merge produces a merged resource and FHIR Patch when the conflict is auto-mergeable.
func (e *Engine) Merge(local LocalEvent, base, current *types.ResourceEnvelope) MergeResult {
	result := e.Detect(local, base, current)
	if !result.AutoMergeable {
		return MergeResult{
			Result:        result,
			AutoMergeable: false,
			Review:        e.buildReview(result),
		}
	}

	mergedJSON, patch, err := rebase(local, base, current, e.cfg.Registry)
	if err != nil {
		result.AutoMergeable = false
		result.Classification = ClassificationUnsupportedMerge
		result.Risk = RiskLevelBlocked
		result.ReviewReason = "merge cannot be expressed as a safe FHIR Patch: " + err.Error()
		return MergeResult{
			Result:        result,
			AutoMergeable: false,
			Review:        e.buildReview(result),
		}
	}

	merged := &types.ResourceEnvelope{
		ResourceType: current.ResourceType,
		ID:           current.ID,
		VersionID:    current.VersionID,
		LastUpdated:  e.now(),
		JSON:         mergedJSON,
	}

	return MergeResult{
		Result:        result,
		AutoMergeable: true,
		Merged:        merged,
		Patch:         patch,
		Resolution:    result.Classification,
	}
}

func (e *Engine) now() time.Time {
	if e == nil || e.cfg.Clock == nil {
		return time.Now().UTC()
	}
	return e.cfg.Clock().UTC()
}

func (e *Engine) buildReview(r Result) ReviewMetadata {
	localSummary := make([]string, 0, len(r.LocalChanges))
	for _, c := range r.LocalChanges {
		localSummary = append(localSummary, string(c.Kind)+": "+strings.Join(c.Path, "."))
	}
	remoteSummary := make([]string, 0, len(r.RemoteChanges))
	for _, c := range r.RemoteChanges {
		remoteSummary = append(remoteSummary, string(c.Kind)+": "+strings.Join(c.Path, "."))
	}
	return ReviewMetadata{
		Reason:            r.ReviewReason,
		LocalPathSummary:  localSummary,
		RemotePathSummary: remoteSummary,
		OverlapPaths:      r.OverlappingPaths,
		UILabels: map[string]string{
			"classification": string(r.Classification),
			"risk":           string(r.Risk),
		},
	}
}

func validLocalEvent(local LocalEvent) bool {
	return local.ResourceType != "" && local.ResourceID != "" && local.Operation != ""
}

func unsupportedResult(reason string) Result {
	return Result{
		Classification: ClassificationUnsupportedMerge,
		Risk:           RiskLevelBlocked,
		AutoMergeable:  false,
		ReviewReason:   reason,
	}
}

func dottedPath(resourceType string, path []string) string {
	if len(path) == 0 {
		return resourceType
	}
	return resourceType + "." + strings.Join(path, ".")
}

func computeOverlaps(local, remote []PathChange, resourceType string) []string {
	seen := make(map[string]bool)
	var overlaps []string
	for _, lc := range local {
		lp := dottedPath(resourceType, lc.Path)
		for _, rc := range remote {
			rp := dottedPath(resourceType, rc.Path)
			if pathsOverlap(lp, rp) && !seen[lp] {
				seen[lp] = true
				overlaps = append(overlaps, lp)
			}
		}
	}
	return overlaps
}

func pathsOverlap(a, b string) bool {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	min := len(partsA)
	if len(partsB) < min {
		min = len(partsB)
	}
	for i := 0; i < min; i++ {
		if partsA[i] != partsB[i] {
			return false
		}
	}
	return true
}

func (e *Engine) riskFor(resourceType string, local, remote []PathChange) RiskLevel {
	for _, changes := range [][]PathChange{local, remote} {
		for _, c := range changes {
			path := dottedPath(resourceType, c.Path)
			rule := e.cfg.Registry.Match(resourceType, path)
			if rule != nil && rule.Semantics == RuleSemanticsBlock {
				return RiskLevelBlocked
			}
			if rule != nil && rule.Semantics == RuleSemanticsReviewOnly {
				return RiskLevelReview
			}
		}
	}
	for _, changes := range [][]PathChange{local, remote} {
		for _, c := range changes {
			path := dottedPath(resourceType, c.Path)
			rule := e.cfg.Registry.Match(resourceType, path)
			if rule != nil && (rule.Semantics == RuleSemanticsAutoMerge || rule.Semantics == RuleSemanticsAppendOnly) {
				return RiskLevelSafe
			}
		}
	}
	return RiskLevelReview
}

func classify(resourceType string, local, remote []PathChange, overlaps []string, registry *PolicyRegistry) Classification {
	if len(local) == 0 && len(remote) == 0 {
		return ClassificationNoConflict
	}
	if len(remote) == 0 || len(local) == 0 {
		return ClassificationStaleBaseOnly
	}
	if len(overlaps) == 0 {
		return ClassificationSameResourceNonOverlap
	}
	for _, ov := range overlaps {
		rule := registry.Match(resourceType, ov)
		if rule == nil || rule.Semantics != RuleSemanticsAppendOnly {
			return ClassificationSameResourceOverlap
		}
		if !allChangesAtPathAreKind(resourceType, local, ov, ChangeKindArrayAppend) {
			return ClassificationSameResourceOverlap
		}
		if !allChangesAtPathAreKind(resourceType, remote, ov, ChangeKindArrayAppend) {
			return ClassificationSameResourceOverlap
		}
	}
	return ClassificationAppendOnlyCompatible
}

func reviewReason(classification Classification, risk RiskLevel, overlaps []string) string {
	if risk == RiskLevelBlocked {
		return "unsupported or blocked merge shape"
	}
	if risk == RiskLevelReview {
		return "clinically sensitive path requires human review"
	}
	if classification == ClassificationSameResourceOverlap {
		if len(overlaps) > 0 {
			return "overlapping update on " + overlaps[0] + " requires manual review"
		}
		return "overlapping update requires manual review"
	}
	return "human review required by policy"
}

func (e *Engine) canAutoMerge(resourceType string, local, remote []PathChange, overlaps []string, risk RiskLevel) bool {
	if risk != RiskLevelSafe {
		return false
	}
	for _, c := range local {
		switch c.Kind {
		case ChangeKindScalarReplace, ChangeKindArrayAppend:
			// ok
		default:
			return false
		}
		path := dottedPath(resourceType, c.Path)
		rule := e.cfg.Registry.Match(resourceType, path)
		if rule == nil {
			return false
		}
		if rule.Semantics == RuleSemanticsReviewOnly || rule.Semantics == RuleSemanticsBlock {
			return false
		}
		if rule.Semantics == RuleSemanticsAppendOnly && c.Kind != ChangeKindArrayAppend {
			return false
		}
	}
	if len(overlaps) == 0 {
		return true
	}
	for _, ov := range overlaps {
		rule := e.cfg.Registry.Match(resourceType, ov)
		if rule == nil || rule.Semantics != RuleSemanticsAppendOnly {
			return false
		}
		if !allChangesAtPathAreKind(resourceType, local, ov, ChangeKindArrayAppend) {
			return false
		}
		if !allChangesAtPathAreKind(resourceType, remote, ov, ChangeKindArrayAppend) {
			return false
		}
	}
	return true
}

func allChangesAtPathAreKind(resourceType string, changes []PathChange, dotted string, kind ChangeKind) bool {
	found := false
	for _, c := range changes {
		if dottedPath(resourceType, c.Path) != dotted {
			continue
		}
		found = true
		if c.Kind != kind {
			return false
		}
	}
	return found
}
