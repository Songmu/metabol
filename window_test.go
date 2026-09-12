package thresh

import (
	"testing"
	"time"
)

func TestDailyWindowLastComplete(t *testing.T) {
	engine, err := NewDailyWindow(time.UTC, "07:00")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		reference string
		wantStart string
		wantEnd   string
	}{
		{
			name:      "normal run",
			reference: "2026-09-11T19:00:00Z",
			wantStart: "2026-09-10T07:00:00Z",
			wantEnd:   "2026-09-11T07:00:00Z",
		},
		{
			name:      "exact boundary",
			reference: "2026-09-11T07:00:00Z",
			wantStart: "2026-09-10T07:00:00Z",
			wantEnd:   "2026-09-11T07:00:00Z",
		},
		{
			name:      "crosses year and month",
			reference: "2026-01-01T12:00:00Z",
			wantStart: "2025-12-31T07:00:00Z",
			wantEnd:   "2026-01-01T07:00:00Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Select(mustParseTime(t, tt.reference), nil)
			if err != nil {
				t.Fatal(err)
			}
			assertWindow(t, got, tt.wantStart, tt.wantEnd)
		})
	}
}

func TestSelectDailyWindowAcceptsResolvedConfiguration(t *testing.T) {
	got, err := SelectDailyWindow(
		time.UTC,
		DailyTime{Hour: 7},
		mustParseTime(t, "2026-09-11T19:00:00Z"),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertWindow(t, got, "2026-09-10T07:00:00Z", "2026-09-11T07:00:00Z")
}

func TestDailyWindowSelectManyReturnsOldestFirst(t *testing.T) {
	engine, err := NewDailyWindow(time.UTC, "07:00")
	if err != nil {
		t.Fatal(err)
	}

	windows, err := engine.SelectMany(
		mustParseTime(t, "2026-09-12T19:00:00Z"),
		nil,
		3,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(windows), 3; got != want {
		t.Fatalf("len(windows) = %d, want %d", got, want)
	}
	assertWindow(t, windows[0], "2026-09-09T07:00:00Z", "2026-09-10T07:00:00Z")
	assertWindow(t, windows[1], "2026-09-10T07:00:00Z", "2026-09-11T07:00:00Z")
	assertWindow(t, windows[2], "2026-09-11T07:00:00Z", "2026-09-12T07:00:00Z")
}

func TestDailyWindowSelectManyStartsWithContainingWindow(t *testing.T) {
	engine, err := NewDailyWindow(time.UTC, "07:00")
	if err != nil {
		t.Fatal(err)
	}
	at := mustParseTime(t, "2026-09-11T19:00:00Z")

	windows, err := engine.SelectMany(time.Time{}, &at, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertWindow(t, windows[0], "2026-09-10T07:00:00Z", "2026-09-11T07:00:00Z")
	assertWindow(t, windows[1], "2026-09-11T07:00:00Z", "2026-09-12T07:00:00Z")
}

func TestDailyWindowSelectManyRejectsInvalidCount(t *testing.T) {
	engine, err := NewDailyWindow(time.UTC, "07:00")
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{0, MaxWindowCount + 1} {
		if _, err := engine.SelectMany(time.Now(), nil, count); err == nil {
			t.Fatalf("SelectMany accepted count %d", count)
		}
	}
}

func TestDailyWindowContaining(t *testing.T) {
	engine, err := NewDailyWindow(time.UTC, "07:00")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		at        string
		wantStart string
		wantEnd   string
	}{
		{
			name:      "inside window",
			at:        "2026-09-11T19:00:00Z",
			wantStart: "2026-09-11T07:00:00Z",
			wantEnd:   "2026-09-12T07:00:00Z",
		},
		{
			name:      "exact boundary belongs to following window",
			at:        "2026-09-11T07:00:00Z",
			wantStart: "2026-09-11T07:00:00Z",
			wantEnd:   "2026-09-12T07:00:00Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := mustParseTime(t, tt.at)
			got, err := engine.Select(time.Time{}, &at)
			if err != nil {
				t.Fatal(err)
			}
			assertWindow(t, got, tt.wantStart, tt.wantEnd)
		})
	}
}

func TestDailyWindowNewYorkOverlapUsesEarlierInstant(t *testing.T) {
	location := mustLoadLocation(t, "America/New_York")
	engine, err := NewDailyWindow(location, "01:30")
	if err != nil {
		t.Fatal(err)
	}

	firstOccurrence := mustParseTime(t, "2026-11-01T05:30:00Z")
	got, err := engine.Containing(firstOccurrence)
	if err != nil {
		t.Fatal(err)
	}
	assertWindow(t, got, "2026-11-01T05:30:00Z", "2026-11-02T06:30:00Z")

	secondOccurrence := mustParseTime(t, "2026-11-01T06:30:00Z")
	got, err = engine.Containing(secondOccurrence)
	if err != nil {
		t.Fatal(err)
	}
	assertWindow(t, got, "2026-11-01T05:30:00Z", "2026-11-02T06:30:00Z")
}

func TestDailyWindowNewYorkGapShiftsByTransition(t *testing.T) {
	location := mustLoadLocation(t, "America/New_York")
	engine, err := NewDailyWindow(location, "02:30")
	if err != nil {
		t.Fatal(err)
	}

	beforeShiftedBoundary := mustParseTime(t, "2026-03-08T07:29:59Z")
	got, err := engine.Containing(beforeShiftedBoundary)
	if err != nil {
		t.Fatal(err)
	}
	assertWindow(t, got, "2026-03-07T07:30:00Z", "2026-03-08T07:30:00Z")

	shiftedBoundary := mustParseTime(t, "2026-03-08T07:30:00Z")
	got, err = engine.Containing(shiftedBoundary)
	if err != nil {
		t.Fatal(err)
	}
	assertWindow(t, got, "2026-03-08T07:30:00Z", "2026-03-09T06:30:00Z")
}

func TestDailyWindowCollapsesDuplicateResolvedBoundaries(t *testing.T) {
	location := mustLoadLocation(t, "Pacific/Apia")
	engine, err := NewDailyWindow(location, "00:00")
	if err != nil {
		t.Fatal(err)
	}

	at := mustParseTime(t, "2011-12-30T10:00:00Z")
	got, err := engine.Containing(at)
	if err != nil {
		t.Fatal(err)
	}
	assertWindow(t, got, "2011-12-30T10:00:00Z", "2011-12-31T10:00:00Z")
}

func TestParseWindowAt(t *testing.T) {
	location := mustLoadLocation(t, "America/New_York")
	tests := []struct {
		name         string
		value        string
		want         string
		wantLocation *time.Location
	}{
		{
			name:         "date only uses configured timezone",
			value:        "2026-03-08",
			want:         "2026-03-08T05:00:00Z",
			wantLocation: location,
		},
		{
			name:  "RFC3339 keeps the instant",
			value: "2026-09-11T12:34:56-04:00",
			want:  "2026-09-11T16:34:56Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseWindowAt(tt.value, location)
			if err != nil {
				t.Fatal(err)
			}
			if want := mustParseTime(t, tt.want); !got.Equal(want) {
				t.Fatalf("ParseWindowAt(%q) = %s, want %s", tt.value, got, want)
			}
			if tt.wantLocation != nil && got.Location() != tt.wantLocation {
				t.Fatalf("ParseWindowAt(%q) location = %s, want %s", tt.value, got.Location(), tt.wantLocation)
			}
		})
	}
}

func TestParseWindowAtRejectsInvalidValue(t *testing.T) {
	if _, err := ParseWindowAt("last-week", time.UTC); err == nil {
		t.Fatal("ParseWindowAt accepted an invalid value")
	}
}

func assertWindow(t *testing.T, got Window, wantStart, wantEnd string) {
	t.Helper()
	start := mustParseTime(t, wantStart)
	end := mustParseTime(t, wantEnd)
	if !got.Start.Equal(start) || !got.End.Equal(end) {
		t.Fatalf("window = [%s, %s), want [%s, %s)", got.Start, got.End, start, end)
	}
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
