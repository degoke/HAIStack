package sdc

import "strings"

func unitOpenAllows(item *Item, qty simpleQuantity) bool {
	mode := strings.ToLower(strings.TrimSpace(item.UnitOpen))
	switch mode {
	case "options-or-unit":
		return item.Unit != nil && item.Unit.Code != "" && qty.Code == item.Unit.Code
	case "options-or-string":
		return qty.Code != ""
	default:
		return false
	}
}
