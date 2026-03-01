package vrs

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// datePattern matches headings that embed a VRS snapshot date.
// Handles formats:
//   - "### Standings as of 2026_02_02"
//   - "## Global Standings - 2026-02-02"
//   - "# 2026_02_02"
var datePattern = regexp.MustCompile(
	`(?i)(?:standings(?:\s+as\s+of)?|global\s+standings\s*[-–]?)?\s*(\d{4})[_\-](\d{2})[_\-](\d{2})`)

// tableRowPattern matches a VRS markdown table data row with at least 4 columns.
// Column layout: | rank | points | team | roster | [optional more] |
// Rank is numeric; points is numeric (may include decimals).
var tableRowPattern = regexp.MustCompile(
	`^\|\s*(\d+)\s*\|\s*([\d.]+)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|`)

// linkPattern strips markdown hyperlink syntax "[text](url)" → "text".
var linkPattern = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)

// ParseGlobalStandings parses a VRS standings markdown file.
// It returns the snapshot date and all team entries found in the file.
// Returns an error if the date cannot be extracted or no data rows are found.
func ParseGlobalStandings(content string) (VRSSnapshot, error) {
	lines := strings.Split(content, "\n")

	var date string
	var entries []VRSEntry

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)

		// Extract date from heading lines (lines starting with #).
		if date == "" && strings.HasPrefix(line, "#") {
			if m := datePattern.FindStringSubmatch(line); m != nil {
				date = fmt.Sprintf("%s-%s-%s", m[1], m[2], m[3])
			}
		}

		// Skip non-table lines and separator rows.
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") {
			continue
		}

		m := tableRowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		rank, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		points, err := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
		if err != nil {
			continue
		}
		teamName := strings.TrimSpace(m[3])
		// Skip header rows where the "rank" column contains text like "Rank" or "1".
		// A valid rank column is already parsed as an integer above, so we only
		// need to skip rows where the team name looks like a column header label.
		if strings.EqualFold(teamName, "team") || strings.EqualFold(teamName, "name") {
			continue
		}

		roster := parseRoster(m[4])
		if len(roster) == 0 {
			continue
		}

		entries = append(entries, VRSEntry{
			SnapshotDate: date,
			Rank:         rank,
			TeamName:     teamName,
			Points:       points,
			Roster:       roster,
		})
	}

	if date == "" {
		return VRSSnapshot{}, fmt.Errorf("no snapshot date found in VRS standings content")
	}
	if len(entries) == 0 {
		return VRSSnapshot{}, fmt.Errorf("no team entries parsed from VRS standings (date=%s)", date)
	}

	return VRSSnapshot{Date: date, Entries: entries}, nil
}

// parseRoster splits a comma-separated roster string into trimmed player names.
// Markdown link syntax "[Name](url)" is unwrapped to just "Name".
func parseRoster(raw string) []string {
	// Unwrap markdown links.
	raw = linkPattern.ReplaceAllString(raw, "$1")

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}
