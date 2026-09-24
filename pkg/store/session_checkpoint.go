package store

// LatestCompactionEvent returns the newest compaction checkpoint in append order.
func LatestCompactionEvent(events []SessionEvent) (SessionEvent, bool) {
	var last SessionEvent
	found := false
	for _, ev := range events {
		if ev.Partial {
			continue
		}
		if ev.Author == SessionAuthorCompaction {
			last = ev
			found = true
		}
	}
	return last, found
}
