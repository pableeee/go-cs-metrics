package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pable/go-cs-metrics/internal/storage"
)

func keys(rows []storage.DeathBreakdown) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Key
	}
	return out
}

func TestSortDeathsByPhase(t *testing.T) {
	// Query returns count-descending; the table should read chronologically.
	rows := []storage.DeathBreakdown{
		{Key: "late"}, {Key: "post_plant"}, {Key: "pistol"}, {Key: "mid"}, {Key: "early"},
	}
	SortDeathsByPhase(rows)
	want := []string{"pistol", "early", "mid", "late", "post_plant"}
	if got := keys(rows); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("phase order = %v, want %v", got, want)
	}
}

func TestSortDeathsByPhase_UnknownSortsLast(t *testing.T) {
	rows := []storage.DeathBreakdown{
		{Key: "weird"}, {Key: "late"}, {Key: "pistol"},
	}
	SortDeathsByPhase(rows)
	if rows[len(rows)-1].Key != "weird" {
		t.Errorf("unknown phase should sort last, got %v", keys(rows))
	}
}

func TestSortDeathsByDistance(t *testing.T) {
	rows := []storage.DeathBreakdown{
		{Key: "30m+"}, {Key: "5-10m"}, {Key: "0-5m"}, {Key: "20-30m"},
		{Key: "15-20m"}, {Key: "10-15m"},
	}
	SortDeathsByDistance(rows)
	want := []string{"0-5m", "5-10m", "10-15m", "15-20m", "20-30m", "30m+"}
	if got := keys(rows); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("distance order = %v, want %v", got, want)
	}
}

func TestPctOf(t *testing.T) {
	if got := pctOf(0, 0); got != "—" {
		t.Errorf("pctOf(0,0) = %q, want em dash (no division by zero)", got)
	}
	if got := pctOf(1, 4); got != "25.0%" {
		t.Errorf("pctOf(1,4) = %q, want 25.0%%", got)
	}
}

// TestPrintDeathTotals_NoRoundsDenominator verifies that a phase-restricted
// view (roundsPlayed = 0) renders ROUNDS and DPR as "—" rather than a
// misleading zero.
func TestPrintDeathTotals_NoRoundsDenominator(t *testing.T) {
	var buf bytes.Buffer
	PrintDeathTotals(&buf, storage.DeathTotals{Deaths: 15, Headshots: 14}, 0, "tester")
	out := buf.String()
	if strings.Contains(out, "0.00") {
		t.Errorf("DPR should be blank without a rounds denominator, got:\n%s", out)
	}
	if !strings.Contains(out, "—") {
		t.Errorf("expected em dash placeholders, got:\n%s", out)
	}
}

func TestPrintDeathBreakdown_EmptyRendersNothing(t *testing.T) {
	var buf bytes.Buffer
	PrintDeathBreakdown(&buf, "Title", "KEY", "desc", nil, 0)
	if buf.Len() != 0 {
		t.Errorf("empty breakdown should render nothing, got:\n%s", buf.String())
	}
}
