package usage

import (
	"strings"
	"testing"
	"time"
)

func TestBuildWhereClauseStartTimeOverridesDays(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	where, args := buildWhereClause(LogQueryParams{
		Days:      7,
		StartTime: &start,
		EndTime:   &end,
	})
	if !strings.Contains(where, "timestamp >= ?") || !strings.Contains(where, "timestamp < ?") {
		t.Fatalf("expected both bounds in where clause, got %q", where)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args (tenant, start, end), got %d: %v", len(args), args)
	}
	if got := args[1].(string); got != "2026-08-01T00:00:00Z" {
		t.Errorf("start arg = %q, want 2026-08-01T00:00:00Z", got)
	}
	if got := args[2].(string); got != "2026-08-02T00:00:00Z" {
		t.Errorf("end arg = %q, want 2026-08-02T00:00:00Z", got)
	}
}

func TestBuildWhereClauseWithoutWindowUsesDays(t *testing.T) {
	where, args := buildWhereClause(LogQueryParams{Days: 7})
	if !strings.Contains(where, "timestamp >= ?") {
		t.Fatalf("expected lower bound, got %q", where)
	}
	if strings.Contains(where, "timestamp < ?") {
		t.Fatalf("did not expect an upper bound, got %q", where)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args (tenant, days cutoff), got %d: %v", len(args), args)
	}
}
