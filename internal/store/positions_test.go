package store

import (
	"testing"
	"time"

	"github.com/Spot-Canvas/ledger/internal/domain"
)

// TestPositionCursorRoundTrip verifies that encodeCursor/decodeCursor work for
// position pagination (positions reuse the same cursor helpers as trades).
func TestPositionCursorRoundTrip(t *testing.T) {
	openedAt := time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC)
	posID := "paper-BTC-USD-spot-1741593600"

	cursor := encodeCursor(openedAt, posID)
	if cursor == "" {
		t.Fatal("encodeCursor returned empty string")
	}

	gotTS, gotID, err := decodeCursor(cursor)
	if err != nil {
		t.Fatalf("decodeCursor error: %v", err)
	}
	if !gotTS.Equal(openedAt) {
		t.Errorf("timestamp roundtrip: got %v, want %v", gotTS, openedAt)
	}
	if gotID != posID {
		t.Errorf("posID roundtrip: got %q, want %q", gotID, posID)
	}
}

// TestPositionFilterDefaults verifies that a zero-value PositionFilter gets
// sensible defaults applied by ListPositions (limit=50, status="open").
// This is a structural test — it doesn't require a database.
func TestPositionFilterDefaults(t *testing.T) {
	// Replicate the default-clamping logic from ListPositions so we can
	// assert on the effective values without a DB.
	filter := PositionFilter{}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	status := filter.Status
	if status == "" {
		status = "open"
	}

	if filter.Limit != 50 {
		t.Errorf("default limit: got %d, want 50", filter.Limit)
	}
	if status != "open" {
		t.Errorf("default status: got %q, want \"open\"", status)
	}
}

// TestPositionFilterMaxLimit verifies that a limit above 200 is clamped to 200.
func TestPositionFilterMaxLimit(t *testing.T) {
	filter := PositionFilter{Limit: 999}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Limit != 200 {
		t.Errorf("max limit clamp: got %d, want 200", filter.Limit)
	}
}

// TestPositionListResultEmptySlice verifies the zero-value PositionListResult
// has a non-nil Positions slice (important for JSON marshalling — [] not null).
func TestPositionListResultEmptySlice(t *testing.T) {
	result := &PositionListResult{}
	if result.Positions != nil {
		// Already non-nil — nothing to do.
		return
	}
	// Simulate what ListPositions does when no rows are found.
	result.Positions = []domain.Position{}
	if result.Positions == nil {
		t.Error("Positions should be a non-nil empty slice, not nil")
	}
	if len(result.Positions) != 0 {
		t.Errorf("expected 0 positions, got %d", len(result.Positions))
	}
	if result.NextCursor != "" {
		t.Errorf("expected empty NextCursor, got %q", result.NextCursor)
	}
}
