package ai

import "sync/atomic"

// CompactionMetricKind classifies a compaction metrics observation.
type CompactionMetricKind string

const (
	CompactionMetricCheck         CompactionMetricKind = "check"
	CompactionMetricSkippedUnder  CompactionMetricKind = "skipped_under_limit"
	CompactionMetricCompacted     CompactionMetricKind = "compacted"
	CompactionMetricFailed        CompactionMetricKind = "failed"
)

// CompactionMetricEvent is emitted when compaction is evaluated or runs.
type CompactionMetricEvent struct {
	Kind               CompactionMetricKind
	ActiveTokens       int
	MaxContextTokens   int
	EventsSummarized   int
	TokensAfterCompact int
	Err                error
}

// SessionCompactionMetrics is cumulative compaction telemetry for one harness instance.
type SessionCompactionMetrics struct {
	Checks            uint64
	SkippedUnderLimit uint64
	Compactions       uint64
	Failures          uint64
	LastActiveTokens  int
	LastTokensAfter   int
}

type compactionMetrics struct {
	checks            atomic.Uint64
	skipped           atomic.Uint64
	compactions       atomic.Uint64
	failures          atomic.Uint64
	lastActiveTokens  atomic.Int64
	lastTokensAfter   atomic.Int64
}

func (m *compactionMetrics) snapshot() SessionCompactionMetrics {
	return SessionCompactionMetrics{
		Checks:            m.checks.Load(),
		SkippedUnderLimit: m.skipped.Load(),
		Compactions:       m.compactions.Load(),
		Failures:          m.failures.Load(),
		LastActiveTokens:  int(m.lastActiveTokens.Load()),
		LastTokensAfter:   int(m.lastTokensAfter.Load()),
	}
}

func (h *Harness) CompactionMetrics() SessionCompactionMetrics {
	if h == nil || h.compactionMetrics == nil {
		return SessionCompactionMetrics{}
	}
	return h.compactionMetrics.snapshot()
}

func (h *Harness) recordCompactionMetric(ev CompactionMetricEvent) {
	if h == nil {
		return
	}
	if h.compactionMetrics == nil {
		h.compactionMetrics = &compactionMetrics{}
	}
	switch ev.Kind {
	case CompactionMetricCheck:
		h.compactionMetrics.checks.Add(1)
		h.compactionMetrics.lastActiveTokens.Store(int64(ev.ActiveTokens))
	case CompactionMetricSkippedUnder:
		h.compactionMetrics.skipped.Add(1)
		h.compactionMetrics.lastActiveTokens.Store(int64(ev.ActiveTokens))
	case CompactionMetricCompacted:
		h.compactionMetrics.compactions.Add(1)
		h.compactionMetrics.lastActiveTokens.Store(int64(ev.ActiveTokens))
		h.compactionMetrics.lastTokensAfter.Store(int64(ev.TokensAfterCompact))
	case CompactionMetricFailed:
		h.compactionMetrics.failures.Add(1)
	}
	if h.cfg.SessionCompaction.OnMetric != nil {
		h.cfg.SessionCompaction.OnMetric(ev)
	}
}
