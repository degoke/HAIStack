package sdc

import "strings"

func isDisplayOnlyUsageMode(mode string) bool {
	return strings.EqualFold(mode, "display")
}

func isDisplayWhenAnsweredUsageMode(mode string) bool {
	return strings.EqualFold(mode, "display-non-empty")
}

func capturesAnswers(item Item) bool {
	if item.Type == "group" || item.Type == "question" {
		return false
	}
	if item.Type == "display" {
		return false
	}
	switch strings.ToLower(item.UsageMode) {
	case "display", "display-non-empty":
		return false
	}
	return true
}

func fieldVisible(item Item, r QuestionnaireResponse, enabled bool) bool {
	if extensionBool(item.Extension, QuestionnaireHiddenExtension) {
		return false
	}
	switch strings.ToLower(item.UsageMode) {
	case "display", "capture-display":
		return true
	case "display-non-empty":
		return hasAnswersInResponse(r, item.LinkID)
	default:
		return enabled
	}
}

func fieldEnabled(item Item, enabled bool) bool {
	switch strings.ToLower(item.UsageMode) {
	case "display", "display-non-empty":
		return false
	case "capture-display", "capture", "":
		return enabled
	default:
		return enabled
	}
}

func hasAnswersInResponse(r QuestionnaireResponse, linkID string) bool {
	ri := findResponseDeep(r.Item, linkID)
	return ri != nil && hasPresentAnswers(ri.Answer)
}
