package thresh

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Window is a half-open interval [Start, End).
type Window struct {
	Start time.Time
	End   time.Time
}

// Contains reports whether instant belongs to the window.
func (w Window) Contains(instant time.Time) bool {
	return !instant.Before(w.Start) && instant.Before(w.End)
}

// DailyWindow selects windows separated by a fixed local wall-clock time.
type DailyWindow struct {
	location *time.Location
	boundary DailyTime
}

// NewDailyWindow creates a daily window selector in location.
func NewDailyWindow(location *time.Location, boundary string) (*DailyWindow, error) {
	parsed, err := ParseDailyTime(boundary)
	if err != nil {
		return nil, fmt.Errorf("daily window boundary: %w", err)
	}
	return NewDailyWindowFromTime(location, parsed)
}

// NewDailyWindowFromTime creates a selector from an already parsed boundary.
func NewDailyWindowFromTime(location *time.Location, boundary DailyTime) (*DailyWindow, error) {
	if location == nil {
		return nil, errors.New("daily window location must not be nil")
	}
	if boundary.Hour < 0 || boundary.Hour > 23 ||
		boundary.Minute < 0 || boundary.Minute > 59 {
		return nil, fmt.Errorf(
			"invalid daily window boundary %02d:%02d",
			boundary.Hour,
			boundary.Minute,
		)
	}
	return &DailyWindow{location: location, boundary: boundary}, nil
}

// NewDailyWindowForTimezone loads timezone and creates a daily window selector.
func NewDailyWindowForTimezone(timezone, boundary string) (*DailyWindow, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load daily window timezone %q: %w", timezone, err)
	}
	return NewDailyWindow(location, boundary)
}

// SelectDailyWindow creates a selector from resolved configuration and selects
// either the last complete window or, when at is non-nil, its containing window.
func SelectDailyWindow(
	location *time.Location,
	boundary DailyTime,
	reference time.Time,
	at *time.Time,
) (Window, error) {
	daily, err := NewDailyWindowFromTime(location, boundary)
	if err != nil {
		return Window{}, err
	}
	return daily.Select(reference, at)
}

// SelectDailyWindows creates a selector from resolved configuration and
// returns count consecutive windows in chronological order.
func SelectDailyWindows(
	location *time.Location,
	boundary DailyTime,
	reference time.Time,
	at *time.Time,
	count int,
) ([]Window, error) {
	daily, err := NewDailyWindowFromTime(location, boundary)
	if err != nil {
		return nil, err
	}
	return daily.SelectMany(reference, at, count)
}

// Select returns the window containing at when at is non-nil. Otherwise it
// returns the last window whose end is not after reference.
func (d *DailyWindow) Select(reference time.Time, at *time.Time) (Window, error) {
	if at != nil {
		return selectContainingWindow(d, *at)
	}
	return selectLastCompleteWindow(d, reference)
}

// SelectMany returns count consecutive windows ending with the selected
// window, ordered from oldest to newest.
func (d *DailyWindow) SelectMany(reference time.Time, at *time.Time, count int) ([]Window, error) {
	if count < 1 {
		return nil, errors.New("window count must be at least 1")
	}
	selected, err := d.Select(reference, at)
	if err != nil {
		return nil, err
	}
	windows := make([]Window, count)
	windows[count-1] = selected
	for i := count - 2; i >= 0; i-- {
		start, err := d.previousBoundary(windows[i+1].Start, false)
		if err != nil {
			return nil, err
		}
		windows[i], err = newWindow(start, windows[i+1].Start)
		if err != nil {
			return nil, err
		}
	}
	return windows, nil
}

// LastComplete returns the last window whose end is not after reference.
func (d *DailyWindow) LastComplete(reference time.Time) (Window, error) {
	return selectLastCompleteWindow(d, reference)
}

// Containing returns the half-open window containing instant.
func (d *DailyWindow) Containing(instant time.Time) (Window, error) {
	return selectContainingWindow(d, instant)
}

// ParseWindowAt parses an RFC3339 instant or a YYYY-MM-DD date. Date-only
// values denote local midnight in location and use the same DST resolution
// rules as daily boundaries.
func ParseWindowAt(value string, location *time.Location) (time.Time, error) {
	if location == nil {
		return time.Time{}, errors.New("window location must not be nil")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q must be RFC3339 or YYYY-MM-DD: %w", value, err)
	}
	return resolveLocalTime(
		location,
		date.Year(),
		date.Month(),
		date.Day(),
		0,
		0,
		0,
		0,
	)
}

type boundaryGenerator interface {
	previousBoundary(time.Time, bool) (time.Time, error)
	nextBoundary(time.Time) (time.Time, error)
}

func selectLastCompleteWindow(generator boundaryGenerator, reference time.Time) (Window, error) {
	end, err := generator.previousBoundary(reference, true)
	if err != nil {
		return Window{}, err
	}
	start, err := generator.previousBoundary(end, false)
	if err != nil {
		return Window{}, err
	}
	return newWindow(start, end)
}

func selectContainingWindow(generator boundaryGenerator, instant time.Time) (Window, error) {
	start, err := generator.previousBoundary(instant, true)
	if err != nil {
		return Window{}, err
	}
	end, err := generator.nextBoundary(instant)
	if err != nil {
		return Window{}, err
	}
	return newWindow(start, end)
}

func newWindow(start, end time.Time) (Window, error) {
	if !start.Before(end) {
		return Window{}, fmt.Errorf("invalid window boundaries: start %s is not before end %s", start, end)
	}
	return Window{Start: start, End: end}, nil
}

func (d *DailyWindow) previousBoundary(instant time.Time, inclusive bool) (time.Time, error) {
	for span := 2; span <= 32; span *= 2 {
		boundaries, err := d.boundariesAround(instant, span)
		if err != nil {
			return time.Time{}, err
		}
		for i := len(boundaries) - 1; i >= 0; i-- {
			boundary := boundaries[i]
			if boundary.Before(instant) || inclusive && boundary.Equal(instant) {
				return boundary, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no daily boundary found before %s", instant)
}

func (d *DailyWindow) nextBoundary(instant time.Time) (time.Time, error) {
	for span := 2; span <= 32; span *= 2 {
		boundaries, err := d.boundariesAround(instant, span)
		if err != nil {
			return time.Time{}, err
		}
		for _, boundary := range boundaries {
			if boundary.After(instant) {
				return boundary, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no daily boundary found after %s", instant)
}

func (d *DailyWindow) boundariesAround(instant time.Time, span int) ([]time.Time, error) {
	local := instant.In(d.location)
	anchor := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	boundaries := make([]time.Time, 0, span*2+1)
	for offset := -span; offset <= span; offset++ {
		date := anchor.AddDate(0, 0, offset)
		boundary, err := resolveLocalTime(
			d.location,
			date.Year(),
			date.Month(),
			date.Day(),
			d.boundary.Hour,
			d.boundary.Minute,
			0,
			0,
		)
		if err != nil {
			return nil, fmt.Errorf("resolve daily boundary for %s: %w", date.Format(time.DateOnly), err)
		}
		boundaries = append(boundaries, boundary)
	}
	sort.Slice(boundaries, func(i, j int) bool {
		return boundaries[i].Before(boundaries[j])
	})
	return compactTimes(boundaries), nil
}

func compactTimes(values []time.Time) []time.Time {
	compacted := values[:0]
	for _, value := range values {
		if len(compacted) == 0 || !value.Equal(compacted[len(compacted)-1]) {
			compacted = append(compacted, value)
		}
	}
	return compacted
}

// resolveLocalTime maps a wall-clock value to an instant without depending on
// time.Date's undocumented choice during overlaps. Exact matches choose the
// earlier instant; nonexistent wall times shift forward by the transition delta.
func resolveLocalTime(
	location *time.Location,
	year int,
	month time.Month,
	day, hour, minute, second, nanosecond int,
) (time.Time, error) {
	if location == nil {
		return time.Time{}, errors.New("location must not be nil")
	}

	wall := time.Date(year, month, day, hour, minute, second, nanosecond, time.UTC)
	offsets := zoneOffsetsAround(location, wall)
	var exact []time.Time
	var shifted time.Time
	var shiftedWall time.Time
	for _, offset := range offsets {
		candidate := time.Unix(wall.Unix()-int64(offset), int64(nanosecond)).In(location)
		local := candidate.In(location)
		renderedWall := time.Date(
			local.Year(),
			local.Month(),
			local.Day(),
			local.Hour(),
			local.Minute(),
			local.Second(),
			local.Nanosecond(),
			time.UTC,
		)
		if renderedWall.Equal(wall) {
			exact = append(exact, candidate)
			continue
		}
		if renderedWall.After(wall) &&
			(shifted.IsZero() || renderedWall.Before(shiftedWall) ||
				renderedWall.Equal(shiftedWall) && candidate.Before(shifted)) {
			shifted = candidate
			shiftedWall = renderedWall
		}
	}
	if len(exact) != 0 {
		sort.Slice(exact, func(i, j int) bool {
			return exact[i].Before(exact[j])
		})
		return exact[0], nil
	}
	if !shifted.IsZero() {
		return shifted, nil
	}
	return time.Time{}, fmt.Errorf(
		"cannot resolve local time %04d-%02d-%02dT%02d:%02d:%02d in %s",
		year,
		month,
		day,
		hour,
		minute,
		second,
		location,
	)
}

func zoneOffsetsAround(location *time.Location, wall time.Time) []int {
	const (
		searchRadius = 72 * time.Hour
		sampleStep   = 30 * time.Minute
	)
	seen := make(map[int]struct{})
	for delta := -searchRadius; delta <= searchRadius; delta += sampleStep {
		_, offset := wall.Add(delta).In(location).Zone()
		seen[offset] = struct{}{}
	}
	offsets := make([]int, 0, len(seen))
	for offset := range seen {
		offsets = append(offsets, offset)
	}
	sort.Ints(offsets)
	return offsets
}
