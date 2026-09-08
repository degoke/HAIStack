package sdc

import "strings"

// FormMode distinguishes capture-time from display-time questionnaire rendering.
type FormMode string

const (
	FormModeCapture FormMode = "capture"
	FormModeDisplay FormMode = "display"
)

func formModeFromResponse(r QuestionnaireResponse) FormMode {
	switch strings.ToLower(r.Status) {
	case "completed", "amended":
		return FormModeDisplay
	default:
		return FormModeCapture
	}
}

func isDisplayOnlyUsageMode(mode string) bool {
	return strings.EqualFold(mode, "display")
}

func isDisplayWhenAnsweredUsageMode(mode string) bool {
	return strings.EqualFold(mode, "display-non-empty")
}

func usageModeAllowsCapture(mode string, formMode FormMode, r QuestionnaireResponse, item Item) bool {
	switch strings.ToLower(mode) {
	case "", "capture-display", "capture-display-non-empty":
		return true
	case "capture":
		return formMode == FormModeCapture
	case "display", "display-non-empty":
		return false
	default:
		return formMode == FormModeCapture
	}
}

func usageModeAllowsDisplay(mode string, formMode FormMode, r QuestionnaireResponse, item Item) bool {
	switch strings.ToLower(mode) {
	case "", "capture-display":
		return true
	case "display":
		return formMode == FormModeDisplay
	case "display-non-empty", "capture-display-non-empty":
		return formMode == FormModeDisplay && hasAnswersInSubtree(r, item.LinkID)
	case "capture":
		return false
	default:
		return formMode == FormModeDisplay
	}
}

func capturesAnswers(item Item, formMode FormMode, r QuestionnaireResponse) bool {
	if item.Type == "group" || item.Type == "question" {
		return false
	}
	if item.Type == "display" {
		return false
	}
	if item.UsageMode != "" && !usageModeAllowsCapture(item.UsageMode, formMode, r, item) {
		return false
	}
	return true
}

func fieldVisible(item Item, r QuestionnaireResponse, enabled bool) bool {
	formMode := formModeFromResponse(r)
	if extensionBool(item.Extension, QuestionnaireHiddenExtension) {
		return false
	}
	if item.UsageMode != "" {
		return usageModeAllowsDisplay(item.UsageMode, formMode, r, item)
	}
	return enabled || formMode == FormModeDisplay
}

func fieldEnabled(item Item, r QuestionnaireResponse, enabled bool) bool {
	formMode := formModeFromResponse(r)
	if item.UsageMode != "" {
		return usageModeAllowsCapture(item.UsageMode, formMode, r, item) && enabled
	}
	return enabled
}

func hasAnswersInResponse(r QuestionnaireResponse, linkID string) bool {
	ri := findResponseDeep(r.Item, linkID)
	return ri != nil && hasPresentAnswers(ri.Answer)
}

func hasAnswersInSubtree(r QuestionnaireResponse, linkID string) bool {
	ri := findResponseDeep(r.Item, linkID)
	if ri == nil {
		return false
	}
	if hasPresentAnswers(ri.Answer) {
		return true
	}
	var walk func([]ResponseItem) bool
	walk = func(items []ResponseItem) bool {
		for _, child := range items {
			if hasPresentAnswers(child.Answer) {
				return true
			}
			if walk(child.Item) {
				return true
			}
			for _, answer := range child.Answer {
				if walk(answer.Item) {
					return true
				}
			}
		}
		return false
	}
	return walk(ri.Item)
}
