package structuremap

import (
	"fmt"
	"strconv"
	"strings"
)

func latestMapVersion(matches []Map) (Map, error) {
	if len(matches) == 0 {
		return Map{}, fmt.Errorf("no StructureMap matches")
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	best := matches[0]
	bestScore, bestOK := versionScore(best.Version)
	sameVersionCount := 1
	for i := 1; i < len(matches); i++ {
		candidate := matches[i]
		score, ok := versionScore(candidate.Version)
		if !ok || !bestOK {
			return Map{}, fmt.Errorf("ambiguous StructureMap versions for %s: non-semver version", best.URL)
		}
		if score > bestScore {
			best = candidate
			bestScore = score
			sameVersionCount = 1
			continue
		}
		if score == bestScore {
			sameVersionCount++
		}
	}
	if sameVersionCount > 1 {
		return Map{}, fmt.Errorf("ambiguous StructureMap versions for %s", best.URL)
	}
	return best, nil
}

func versionScore(version string) (int64, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return 0, true
	}
	build := version
	if parts := strings.SplitN(version, "+", 2); len(parts) > 0 {
		build = parts[0]
	}
	pre := ""
	if parts := strings.SplitN(build, "-", 2); len(parts) == 2 {
		pre = parts[1]
		build = parts[0]
	}
	segments := strings.Split(build, ".")
	if len(segments) != 3 {
		return 0, false
	}
	major, err1 := strconv.Atoi(segments[0])
	minor, err2 := strconv.Atoi(segments[1])
	patch, err3 := strconv.Atoi(segments[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}
	score := int64(major)*1_000_000_000 + int64(minor)*1_000_000 + int64(patch)*1_000
	if pre != "" {
		score--
	}
	return score, true
}
