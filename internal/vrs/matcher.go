package vrs

import "strings"

// MatchOpponentToVRS identifies which VRS team a set of opponent player names
// belongs to, using the most recent snapshot on or before matchDate.
//
// Matching is case-insensitive. Returns the VRS team name, global rank, and
// ok=true if at least 3 opponent names match any single VRS team's roster.
// A score of exactly 2 (and uniquely the best) is also accepted as a borderline
// match (absorbs stand-ins or accent-stripped name variants).
//
// Returns ok=false if:
//   - No VRS snapshot exists on or before matchDate
//   - The best match count is < 2
//   - Two or more teams tie at count = 2 (ambiguous)
func MatchOpponentToVRS(opponentNames []string, matchDate string, store *VRSStore) (teamName string, rank int, ok bool) {
	entries, err := store.LatestSnapshotBefore(matchDate)
	if err != nil || len(entries) == 0 {
		return "", 0, false
	}

	// Normalise opponent names to lowercase for comparison.
	normNames := make([]string, len(opponentNames))
	for i, n := range opponentNames {
		normNames[i] = strings.ToLower(n)
	}

	bestCount := 0
	var bestEntries []VRSEntry

	for _, e := range entries {
		// Build a lowercase roster lookup for this VRS team.
		rosterSet := make(map[string]bool, len(e.Roster))
		for _, r := range e.Roster {
			rosterSet[strings.ToLower(r)] = true
		}

		count := 0
		for _, n := range normNames {
			if rosterSet[n] {
				count++
			}
		}

		if count == 0 {
			continue
		}
		if count > bestCount {
			bestCount = count
			bestEntries = []VRSEntry{e}
		} else if count == bestCount {
			bestEntries = append(bestEntries, e)
		}
	}

	switch {
	case bestCount >= 3:
		// Strong match — return the top-ranked team among ties (entries are
		// ordered by vrs_rank ascending, so the first tie entry is highest ranked).
		return bestEntries[0].TeamName, bestEntries[0].Rank, true
	case bestCount == 2 && len(bestEntries) == 1:
		// Borderline match — unique team with 2 matching names.
		return bestEntries[0].TeamName, bestEntries[0].Rank, true
	default:
		return "", 0, false
	}
}
