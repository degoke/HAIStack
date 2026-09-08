package sdc

import (
	"github.com/google/uuid"
)

func allocateExtractIDs(items []Item, responseItems []ResponseItem, scope map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range scope {
		out[k] = v
	}
	var walk func([]Item, []ResponseItem)
	walk = func(qItems []Item, rItems []ResponseItem) {
		for _, item := range qItems {
			if item.ExtractAllocateID != "" {
				out[item.ExtractAllocateID] = "urn:uuid:" + uuid.NewString()
			}
			ri := findResponse(rItems, item.LinkID)
			childResponse := []ResponseItem{}
			if ri != nil {
				childResponse = ri.Item
			}
			walk(item.Item, childResponse)
		}
	}
	walk(items, responseItems)
	return out
}

func extractScopeForItem(q Questionnaire, item Item, responseItems []ResponseItem, parentScope map[string]any) map[string]any {
	ancestors := questionnaireAncestors(q, item.LinkID)
	items := append(ancestors, item)
	return allocateExtractIDs(items, responseItems, parentScope)
}
