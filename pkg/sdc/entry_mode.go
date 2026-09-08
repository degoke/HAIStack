package sdc

import "strings"

func validateEntryMode(q Questionnaire, r QuestionnaireResponse, opts ValidationOptions, o *Outcome) {
	mode := strings.ToLower(strings.TrimSpace(q.EntryMode))
	if mode == "" || mode == "prior-edit" || mode == "random" {
		return
	}
	if mode != "sequential" {
		return
	}
	order := orderedCaptureItems(q, r, opts)
	maxAnswered := -1
	for i, linkID := range order {
		if hasAnswersInResponse(r, linkID) {
			maxAnswered = i
		}
	}
	for i := 0; i < maxAnswered; i++ {
		linkID := order[i]
		if hasAnswersInResponse(r, linkID) {
			continue
		}
		item := itemByLinkID(q, linkID)
		if item == nil {
			continue
		}
		if enabledForValidation(*item, r, opts) {
			o.add("error", "invariant", "sequential entry mode requires prior items to be answered before later items", "QuestionnaireResponse.item["+linkID+"]")
		}
	}
}

func orderedCaptureItems(q Questionnaire, r QuestionnaireResponse, opts ValidationOptions) []string {
	var order []string
	var walk func([]Item)
	walk = func(items []Item) {
		for _, item := range items {
			if item.Type != "group" && item.Type != "question" && item.Type != "display" {
				if capturesAnswers(item, formModeFromResponse(r), r) && enabledForValidation(item, r, opts) {
					order = append(order, item.LinkID)
				}
			}
			walk(item.Item)
		}
	}
	walk(q.Item)
	return order
}

func itemByLinkID(q Questionnaire, linkID string) *Item {
	t, err := Normalize(q)
	if err != nil {
		return nil
	}
	defs := t.Resolve(linkID)
	if len(defs) == 0 {
		return nil
	}
	return defs[0]
}
