package sdc

import (
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ItemPath identifies a location in the response tree. Each segment names a
// parent item by linkId and, for repeating parents, the zero-based instance
// index at that level.
type ItemPath []PathSegment

// PathSegment is one level in an ItemPath.
type PathSegment struct {
	LinkID string
	Index  int
}

// ResponseBuilder constructs a QuestionnaireResponse from rendered form values
// while applying SDC answer typing rules. Use NewResponse to create a builder,
// set answers with Set or the typed helpers, then call Build to validate and
// obtain the response.
type ResponseBuilder struct {
	questionnaire  Questionnaire
	tree           Tree
	status         string
	subject        map[string]any
	authored       string
	root           []ResponseItem
	groupInstances map[string]int
	issues         Outcome
}

// NewResponse creates a builder for the given questionnaire. The builder sets
// QuestionnaireResponse.questionnaire to Canonical(q) and defaults status to
// in-progress.
func NewResponse(q Questionnaire) (*ResponseBuilder, error) {
	tree, err := Normalize(q)
	if err != nil {
		return nil, err
	}
	return &ResponseBuilder{
		questionnaire: q,
		tree:          tree,
		status:        "in-progress",
		issues:        Outcome{ResourceType: "OperationOutcome"},
	}, nil
}

// SetStatus sets QuestionnaireResponse.status.
func (b *ResponseBuilder) SetStatus(status string) *ResponseBuilder {
	b.status = status
	return b
}

// SetSubject sets QuestionnaireResponse.subject.
func (b *ResponseBuilder) SetSubject(subject map[string]any) *ResponseBuilder {
	b.subject = subject
	return b
}

// SetAuthored sets QuestionnaireResponse.authored.
func (b *ResponseBuilder) SetAuthored(authored string) *ResponseBuilder {
	b.authored = authored
	return b
}

// InGroup selects the repeating-group instance used by subsequent auto-placed
// Set, AppendAnswer, and SetCoding calls for nested linkIds.
func (b *ResponseBuilder) InGroup(linkID string, index int) *ResponseBuilder {
	if b.groupInstances == nil {
		b.groupInstances = make(map[string]int)
	}
	b.groupInstances[linkID] = index
	return b
}

// Set assigns an answer for linkID. When linkID is unique in the questionnaire
// the answer is placed at the correct nesting level automatically. When linkID
// appears at multiple levels, use SetAt or SetAtAnswer with an explicit path.
func (b *ResponseBuilder) Set(linkID string, value any) *ResponseBuilder {
	parentPath, underAnswer, ok, ambiguous := b.resolveTarget(linkID)
	switch {
	case ambiguous:
		b.addBuilderIssue("error", "duplicate", "ambiguous linkId: "+linkID+" (use SetAt or SetAtAnswer with an explicit path)", fieldPath(linkID))
	case !ok:
		b.addBuilderIssue("error", "not-found", "unknown linkId: "+linkID, fieldPath(linkID))
	case underAnswer:
		b.setAtAnswer(parentPath, 0, linkID, value, false)
	default:
		b.setAt(parentPath, linkID, value, false)
	}
	return b
}

// SetAt assigns an answer for linkID under the given parent path.
func (b *ResponseBuilder) SetAt(path ItemPath, linkID string, value any) *ResponseBuilder {
	b.setAt(path, linkID, value, false)
	return b
}

// SetAtAnswer assigns an answer for linkID nested under answer[answerIndex] of
// the controlling item identified by path. path must include the controlling
// question as its final segment.
func (b *ResponseBuilder) SetAtAnswer(path ItemPath, answerIndex int, linkID string, value any) *ResponseBuilder {
	b.setAtAnswer(path, answerIndex, linkID, value, false)
	return b
}

// AppendAnswer adds another answer for a repeating question identified by
// linkID. When linkID is unique the nesting level is resolved automatically.
func (b *ResponseBuilder) AppendAnswer(linkID string, value any) *ResponseBuilder {
	parentPath, underAnswer, ok, ambiguous := b.resolveTarget(linkID)
	switch {
	case ambiguous:
		b.addBuilderIssue("error", "duplicate", "ambiguous linkId: "+linkID+" (use AppendAnswerAt or AppendAnswerAtAnswer with an explicit path)", fieldPath(linkID))
	case !ok:
		b.addBuilderIssue("error", "not-found", "unknown linkId: "+linkID, fieldPath(linkID))
	case underAnswer:
		b.setAtAnswer(parentPath, 0, linkID, value, true)
	default:
		b.setAt(parentPath, linkID, value, true)
	}
	return b
}

// AppendAnswerAt adds another answer for a repeating question at the given path.
func (b *ResponseBuilder) AppendAnswerAt(path ItemPath, linkID string, value any) *ResponseBuilder {
	b.setAt(path, linkID, value, true)
	return b
}

// AppendAnswerAtAnswer adds another answer for a repeating question nested
// under answer[answerIndex] of the controlling item identified by path.
func (b *ResponseBuilder) AppendAnswerAtAnswer(path ItemPath, answerIndex int, linkID string, value any) *ResponseBuilder {
	b.setAtAnswer(path, answerIndex, linkID, value, true)
	return b
}

// SetCoding assigns a choice or open-choice answer by option code. The code is
// resolved to the declared answerOption Coding when present; otherwise an issue
// is recorded for closed choice items.
func (b *ResponseBuilder) SetCoding(linkID, code string) *ResponseBuilder {
	return b.SetCodingWithSystem(linkID, "", code)
}

// SetCodingWithSystem assigns a choice answer by code and, when system is
// non-empty, requires the matching answerOption system.
func (b *ResponseBuilder) SetCodingWithSystem(linkID, system, code string) *ResponseBuilder {
	parentPath, underAnswer, ok, ambiguous := b.resolveTarget(linkID)
	switch {
	case ambiguous:
		b.addBuilderIssue("error", "duplicate", "ambiguous linkId: "+linkID+" (use SetCodingAt or SetCodingAtAnswer with an explicit path)", fieldPath(linkID))
	case !ok:
		b.addBuilderIssue("error", "not-found", "unknown linkId: "+linkID, fieldPath(linkID))
	case underAnswer:
		b.setCodingAtAnswer(parentPath, 0, linkID, system, code, false)
	default:
		b.setCodingAt(parentPath, linkID, system, code, false)
	}
	return b
}

// SetCodingAt assigns a choice answer by option code at the given path.
func (b *ResponseBuilder) SetCodingAt(path ItemPath, linkID, code string) *ResponseBuilder {
	return b.SetCodingAtWithSystem(path, linkID, "", code)
}

// SetCodingAtWithSystem assigns a choice answer by code and optional system at
// the given path.
func (b *ResponseBuilder) SetCodingAtWithSystem(path ItemPath, linkID, system, code string) *ResponseBuilder {
	b.setCodingAt(path, linkID, system, code, false)
	return b
}

// SetCodingAtAnswer assigns a choice answer nested under answer[answerIndex].
func (b *ResponseBuilder) SetCodingAtAnswer(path ItemPath, answerIndex int, linkID, code string) *ResponseBuilder {
	return b.SetCodingAtAnswerWithSystem(path, answerIndex, linkID, "", code)
}

// SetCodingAtAnswerWithSystem assigns a choice answer nested under
// answer[answerIndex] with optional system matching.
func (b *ResponseBuilder) SetCodingAtAnswerWithSystem(path ItemPath, answerIndex int, linkID, system, code string) *ResponseBuilder {
	b.setCodingAtAnswer(path, answerIndex, linkID, system, code, false)
	return b
}

// Preview returns the in-progress response without running validation.
func (b *ResponseBuilder) Preview() QuestionnaireResponse {
	return QuestionnaireResponse{
		ResourceType:  "QuestionnaireResponse",
		Questionnaire: Canonical(b.questionnaire),
		Status:        b.status,
		Subject:       b.subject,
		Authored:      b.authored,
		Item:          append([]ResponseItem(nil), b.root...),
	}
}

// Build assembles and validates the QuestionnaireResponse. It returns
// ValidationError when builder or SDC validation reports error issues.
func (b *ResponseBuilder) Build(opts ValidationOptions) (QuestionnaireResponse, error) {
	r := b.Preview()
	outcome := b.issues
	validation := ValidateResponse(b.questionnaire, r, opts)
	outcome.Issue = append(outcome.Issue, validation.Issue...)
	if err := ErrFromOutcome(outcome); err != nil {
		return QuestionnaireResponse{}, err
	}
	return r, nil
}

// BuildResource is like Build but returns a canonical ResourceEnvelope.
func (b *ResponseBuilder) BuildResource(opts ValidationOptions) (*types.ResourceEnvelope, error) {
	r, err := b.Build(opts)
	if err != nil {
		return nil, err
	}
	return ResponseProjectionEnvelope(r)
}

// BuilderOutcome returns issues recorded during Set operations before Build
// validation. Most callers should use Build and handle ValidationError instead.
func (b *ResponseBuilder) BuilderOutcome() Outcome {
	return b.issues
}

func (b *ResponseBuilder) resolveTarget(linkID string) (parentPath ItemPath, underAnswer bool, ok bool, ambiguous bool) {
	groupParents := parentPaths(b.questionnaire.Item, linkID, nil, b.segmentIndex)
	answerParents := answerParentPaths(b.questionnaire.Item, linkID, nil, b.segmentIndex)
	total := len(groupParents) + len(answerParents)
	switch total {
	case 0:
		return nil, false, false, false
	case 1:
		if len(groupParents) == 1 {
			return groupParents[0], false, true, false
		}
		return answerParents[0], true, true, false
	default:
		return nil, false, false, true
	}
}

func (b *ResponseBuilder) segmentIndex(linkID string) int {
	if b.groupInstances != nil {
		if index, ok := b.groupInstances[linkID]; ok {
			return index
		}
	}
	return 0
}

func (b *ResponseBuilder) setCodingAt(path ItemPath, linkID, system, code string, appendAnswer bool) {
	item := b.resolveItemDef(path, linkID)
	if item == nil {
		b.addBuilderIssue("error", "structure", "response item is not allowed at this level", fieldPathAt(path, linkID))
		return
	}
	switch item.Type {
	case "choice", "open-choice":
	default:
		b.addBuilderIssue("error", "type", "SetCoding requires a choice item", fieldPathAt(path, linkID))
		return
	}
	if c, ok := matchChoiceOption(item, code, system); ok {
		b.placeAnswer(path, linkID, item, Answer{Value: c, ValueType: "Coding"}, appendAnswer)
		return
	}
	if item.Type == "open-choice" {
		b.placeAnswer(path, linkID, item, Answer{Value: code, ValueType: "String"}, appendAnswer)
		return
	}
	b.addBuilderIssue("error", "code-invalid", "answer is not one of the permitted answer options", fieldPathAt(path, linkID))
}

func (b *ResponseBuilder) setCodingAtAnswer(path ItemPath, answerIndex int, linkID, system, code string, appendAnswer bool) {
	item := b.resolveItemDefUnderAnswer(path, linkID)
	if item == nil {
		b.addBuilderIssue("error", "structure", "response item is not allowed at this level", fieldPathAtAnswer(path, answerIndex, linkID))
		return
	}
	switch item.Type {
	case "choice", "open-choice":
	default:
		b.addBuilderIssue("error", "type", "SetCoding requires a choice item", fieldPathAtAnswer(path, answerIndex, linkID))
		return
	}
	if c, ok := matchChoiceOption(item, code, system); ok {
		b.placeAnswerAtAnswer(path, answerIndex, linkID, item, Answer{Value: c, ValueType: "Coding"}, appendAnswer)
		return
	}
	if item.Type == "open-choice" {
		b.placeAnswerAtAnswer(path, answerIndex, linkID, item, Answer{Value: code, ValueType: "String"}, appendAnswer)
		return
	}
	b.addBuilderIssue("error", "code-invalid", "answer is not one of the permitted answer options", fieldPathAtAnswer(path, answerIndex, linkID))
}

func (b *ResponseBuilder) setAt(path ItemPath, linkID string, value any, appendAnswer bool) {
	item := b.resolveItemDef(path, linkID)
	if item == nil {
		if len(b.tree.Resolve(linkID)) == 0 {
			b.addBuilderIssue("error", "not-found", "unknown linkId: "+linkID, fieldPathAt(path, linkID))
		} else {
			b.addBuilderIssue("error", "structure", "response item is not allowed at this level", fieldPathAt(path, linkID))
		}
		return
	}
	if item.Type == "group" || item.Type == "display" {
		b.addBuilderIssue("error", "type", "group and display items cannot have answers", fieldPathAt(path, linkID))
		return
	}
	answer, issue := prepareBuilderAnswer(item, value, "")
	if issue != "" {
		b.addBuilderIssue("error", builderIssueCode(item, value), issue, fieldPathAt(path, linkID))
		return
	}
	if answerValueAbsent(answer.Value) {
		return
	}
	b.placeAnswer(path, linkID, item, answer, appendAnswer)
}

func (b *ResponseBuilder) setAtAnswer(path ItemPath, answerIndex int, linkID string, value any, appendAnswer bool) {
	item := b.resolveItemDefUnderAnswer(path, linkID)
	if item == nil {
		if len(b.tree.Resolve(linkID)) == 0 {
			b.addBuilderIssue("error", "not-found", "unknown linkId: "+linkID, fieldPathAtAnswer(path, answerIndex, linkID))
		} else {
			b.addBuilderIssue("error", "structure", "response item is not allowed at this level", fieldPathAtAnswer(path, answerIndex, linkID))
		}
		return
	}
	if item.Type == "group" || item.Type == "display" {
		b.addBuilderIssue("error", "type", "group and display items cannot have answers", fieldPathAtAnswer(path, answerIndex, linkID))
		return
	}
	answer, issue := prepareBuilderAnswer(item, value, "")
	if issue != "" {
		b.addBuilderIssue("error", builderIssueCode(item, value), issue, fieldPathAtAnswer(path, answerIndex, linkID))
		return
	}
	if answerValueAbsent(answer.Value) {
		return
	}
	b.placeAnswerAtAnswer(path, answerIndex, linkID, item, answer, appendAnswer)
}

func (b *ResponseBuilder) placeAnswer(path ItemPath, linkID string, item *Item, answer Answer, appendAnswer bool) {
	container := b.itemsAt(path)
	ri := b.ensureResponseItem(container, linkID, 0, item.Text)
	if appendAnswer {
		if !item.Repeats {
			b.addBuilderIssue("error", "max", "question does not repeat", fieldPathAt(path, linkID))
			return
		}
		ri.Answer = append(ri.Answer, answer)
		return
	}
	ri.Answer = []Answer{answer}
}

func (b *ResponseBuilder) placeAnswerAtAnswer(path ItemPath, answerIndex int, linkID string, item *Item, answer Answer, appendAnswer bool) {
	container, ok := b.itemsAtAnswer(path, answerIndex)
	if !ok {
		b.addBuilderIssue("error", "structure", "response item is not allowed at this level", fieldPathAtAnswer(path, answerIndex, linkID))
		return
	}
	ri := b.ensureResponseItem(container, linkID, 0, item.Text)
	if appendAnswer {
		if !item.Repeats {
			b.addBuilderIssue("error", "max", "question does not repeat", fieldPathAtAnswer(path, answerIndex, linkID))
			return
		}
		ri.Answer = append(ri.Answer, answer)
		return
	}
	ri.Answer = []Answer{answer}
}

func (b *ResponseBuilder) itemsAt(path ItemPath) *[]ResponseItem {
	items := &b.root
	currentDefs := b.questionnaire.Item
	for _, seg := range path {
		text := ""
		for i := range currentDefs {
			if currentDefs[i].LinkID == seg.LinkID {
				text = currentDefs[i].Text
				currentDefs = currentDefs[i].Item
				break
			}
		}
		ri := b.ensureResponseItem(items, seg.LinkID, seg.Index, text)
		items = &ri.Item
	}
	return items
}

func (b *ResponseBuilder) itemsAtAnswer(path ItemPath, answerIndex int) (*[]ResponseItem, bool) {
	if len(path) == 0 {
		return nil, false
	}
	parentPath := path[:len(path)-1]
	parentSeg := path[len(path)-1]
	parentDef := b.resolveItemDef(parentPath, parentSeg.LinkID)
	if parentDef == nil {
		return nil, false
	}
	container := b.itemsAt(parentPath)
	parentItem := b.ensureResponseItem(container, parentSeg.LinkID, parentSeg.Index, parentDef.Text)
	for len(parentItem.Answer) <= answerIndex {
		parentItem.Answer = append(parentItem.Answer, Answer{})
	}
	return &parentItem.Answer[answerIndex].Item, true
}

func (b *ResponseBuilder) ensureResponseItem(items *[]ResponseItem, linkID string, index int, text string) *ResponseItem {
	matches := directResponses(*items, linkID)
	for len(matches) <= index {
		*items = append(*items, ResponseItem{LinkID: linkID, Text: text})
		matches = directResponses(*items, linkID)
	}
	return matches[index]
}

func (b *ResponseBuilder) resolveItemDef(parentPath ItemPath, linkID string) *Item {
	items := b.questionnaire.Item
	for _, seg := range parentPath {
		var next []Item
		found := false
		for i := range items {
			if items[i].LinkID != seg.LinkID {
				continue
			}
			next = items[i].Item
			found = true
			break
		}
		if !found {
			return nil
		}
		items = next
	}
	for i := range items {
		if items[i].LinkID == linkID {
			return &items[i]
		}
	}
	return nil
}

func (b *ResponseBuilder) resolveItemDefUnderAnswer(parentPath ItemPath, linkID string) *Item {
	if len(parentPath) == 0 {
		return nil
	}
	parentSeg := parentPath[len(parentPath)-1]
	parentItem := b.resolveItemDef(parentPath[:len(parentPath)-1], parentSeg.LinkID)
	if parentItem == nil {
		return nil
	}
	for i := range parentItem.Item {
		if parentItem.Item[i].LinkID == linkID {
			return &parentItem.Item[i]
		}
	}
	return nil
}

func (b *ResponseBuilder) addBuilderIssue(severity, code, message, path string) {
	b.issues.add(severity, code, message, path)
}

func parentPaths(items []Item, target string, ancestors ItemPath, indexFor func(linkID string) int) []ItemPath {
	var paths []ItemPath
	for i := range items {
		it := items[i]
		childAncestors := append(append(ItemPath(nil), ancestors...), PathSegment{LinkID: it.LinkID, Index: indexFor(it.LinkID)})
		if it.LinkID == target {
			paths = append(paths, ancestors)
		}
		switch it.Type {
		case "group", "display":
			paths = append(paths, parentPaths(it.Item, target, childAncestors, indexFor)...)
		}
	}
	return paths
}

func answerParentPaths(items []Item, target string, ancestors ItemPath, indexFor func(linkID string) int) []ItemPath {
	var paths []ItemPath
	for i := range items {
		it := items[i]
		pathToItem := append(append(ItemPath(nil), ancestors...), PathSegment{LinkID: it.LinkID, Index: indexFor(it.LinkID)})
		if it.Type != "group" && it.Type != "display" {
			for _, child := range it.Item {
				if child.LinkID == target {
					paths = append(paths, pathToItem)
				}
			}
			paths = append(paths, answerParentPaths(it.Item, target, pathToItem, indexFor)...)
		} else {
			paths = append(paths, answerParentPaths(it.Item, target, pathToItem, indexFor)...)
		}
	}
	return paths
}

func prepareBuilderAnswer(item *Item, value any, system string) (Answer, string) {
	if answerValueAbsent(value) {
		return Answer{}, ""
	}
	if isFormedAnswerValue(item, value) {
		return Answer{Value: value, ValueType: itemValueType(item.Type, value)}, ""
	}
	if item.Type == "choice" {
		if code, ok := value.(string); ok {
			if c, found := matchChoiceOption(item, code, system); found {
				return Answer{Value: c, ValueType: "Coding"}, ""
			}
			return Answer{}, "answer is not one of the permitted answer options"
		}
	}
	if item.Type == "open-choice" {
		if code, ok := value.(string); ok {
			if c, found := matchChoiceOption(item, code, system); found {
				return Answer{Value: c, ValueType: "Coding"}, ""
			}
			return Answer{Value: code, ValueType: "String"}, ""
		}
	}
	if !answerTypeOK(item.Type, value) {
		return Answer{}, "answer type does not match question type"
	}
	return Answer{Value: value, ValueType: itemValueType(item.Type, value)}, ""
}

func builderIssueCode(item *Item, value any) string {
	if item.Type == "choice" {
		if _, ok := value.(string); ok {
			return "code-invalid"
		}
	}
	return "type"
}

func isFormedAnswerValue(item *Item, value any) bool {
	if !answerTypeOK(item.Type, value) {
		return false
	}
	switch item.Type {
	case "choice":
		_, ok := codingFrom(value)
		return ok
	case "open-choice":
		if _, ok := codingFrom(value); ok {
			return true
		}
		_, ok := value.(string)
		return ok
	default:
		return true
	}
}

func matchChoiceOption(item *Item, code, system string) (Coding, bool) {
	var fallback Coding
	var fallbackOK bool
	for _, option := range item.AnswerOption {
		if c, ok := codingFrom(option.Value); ok {
			if !codingOptionMatches(c, code, system) {
				continue
			}
			if system == "" && c.System != "" {
				if !fallbackOK {
					fallback = c
					fallbackOK = true
				}
				continue
			}
			return c, true
		}
		if s, ok := option.Value.(string); ok && s == code && system == "" {
			return Coding{Code: s}, true
		}
	}
	if fallbackOK {
		return fallback, true
	}
	return Coding{}, false
}

func codingOptionMatches(c Coding, code, system string) bool {
	if c.Code != code {
		return false
	}
	if system == "" {
		return true
	}
	return c.System == "" || c.System == system
}

func fieldPath(linkID string) string {
	return "item[" + linkID + "]"
}

func fieldPathAt(path ItemPath, linkID string) string {
	parts := make([]string, 0, len(path)+1)
	for _, seg := range path {
		part := "item[" + seg.LinkID + "]"
		if seg.Index > 0 {
			part = fmt.Sprintf("%s[%d]", part, seg.Index)
		}
		parts = append(parts, part)
	}
	parts = append(parts, "item["+linkID+"]")
	return strings.Join(parts, ".")
}

func fieldPathAtAnswer(path ItemPath, answerIndex int, linkID string) string {
	if len(path) == 0 {
		return fmt.Sprintf("answer[%d].item[%s]", answerIndex, linkID)
	}
	parent := path[len(path)-1]
	parentPrefix := fieldPathAt(path[:len(path)-1], parent.LinkID)
	if parent.Index > 0 {
		parentPrefix = fmt.Sprintf("%s[%d]", parentPrefix, parent.Index)
	}
	return fmt.Sprintf("%s.answer[%d].item[%s]", parentPrefix, answerIndex, linkID)
}
