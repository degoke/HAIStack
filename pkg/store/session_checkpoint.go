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

// ActiveContextView splits history into the latest checkpoint summary and replayable tail events.
// Checkpoints are append-only and may appear after the tail physically; tail selection uses lastCoveredEventId.
func ActiveContextView(events []SessionEvent) (checkpointSummary string, active []SessionEvent) {
	checkpoint, ok := LatestCompactionEvent(events)
	if !ok {
		return "", nonCompactionEvents(events)
	}
	checkpointSummary = checkpoint.Content
	lastCovered := ""
	if checkpoint.Metadata != nil {
		lastCovered = checkpoint.Metadata[SessionMetadataLastCoveredEventID]
	}
	if lastCovered == "" {
		return checkpointSummary, nil
	}
	pastCover := false
	for _, ev := range events {
		if ev.Partial || ev.Author == SessionAuthorCompaction {
			continue
		}
		if !pastCover {
			if ev.ID == lastCovered {
				pastCover = true
			}
			continue
		}
		active = append(active, ev)
	}
	return checkpointSummary, active
}

// ActiveContextEvents returns the minimal event slice stored on the harness after load:
// the latest compaction row (if any) plus tail events after lastCoveredEventId.
func ActiveContextEvents(events []SessionEvent) []SessionEvent {
	summary, active := ActiveContextView(events)
	if summary == "" && len(active) == 0 {
		return append([]SessionEvent(nil), events...)
	}
	out := make([]SessionEvent, 0, len(active)+1)
	if checkpoint, ok := LatestCompactionEvent(events); ok {
		out = append(out, checkpoint)
	}
	out = append(out, active...)
	return out
}

func nonCompactionEvents(events []SessionEvent) []SessionEvent {
	out := make([]SessionEvent, 0, len(events))
	for _, ev := range events {
		if ev.Partial || ev.Author == SessionAuthorCompaction {
			continue
		}
		out = append(out, ev)
	}
	return out
}
