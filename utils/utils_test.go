package utils

import (
	"testing"
	"time"
)

func TestGetLastResetDateKeepsLocalMidnight(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, loc)
	got := GetLastResetDate(15, now)
	want := time.Date(2026, 9, 15, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("GetLastResetDate = %v, want %v", got, want)
	}
}

func TestGetLastResetInstantUsesTimezoneClock(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 38, 11, 0, time.UTC)
	got := GetLastResetInstant(15, "12:38:12", "UTC", now)
	want := time.Date(2026, 8, 15, 12, 38, 12, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("before clock = %v, want %v", got, want)
	}
	at := time.Date(2026, 9, 15, 12, 38, 12, 0, time.UTC)
	got = GetLastResetInstant(15, "12:38:12", "UTC", at)
	want = time.Date(2026, 9, 15, 12, 38, 12, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("at clock = %v, want %v", got, want)
	}
}

func TestGetLastResetInstantEmptyMatchesBeijingMidnight(t *testing.T) {
	// 2026-08-01 00:00 Shanghai is 2026-07-31 16:00 UTC.
	before := time.Date(2026, 7, 31, 15, 59, 0, 0, time.UTC)
	got := GetLastResetInstant(1, "", "", before)
	want := time.Date(2026, 7, 1, 0, 0, 0, 0, time.FixedZone(DefaultResetTimezone, 8*60*60))
	if !got.Equal(want) {
		t.Fatalf("before Beijing midnight = %v, want %v", got, want)
	}
	after := time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC)
	got = GetLastResetInstant(1, "", "", after)
	want = time.Date(2026, 8, 1, 0, 0, 0, 0, time.FixedZone(DefaultResetTimezone, 8*60*60))
	if !got.Equal(want) {
		t.Fatalf("at Beijing midnight = %v, want %v", got, want)
	}
}

func TestGetLastResetInstantClampsFebruary(t *testing.T) {
	loc := time.FixedZone(DefaultResetTimezone, 8*60*60)
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, loc)
	got := GetLastResetInstant(31, "00:00:00", DefaultResetTimezone, now)
	want := time.Date(2026, 2, 28, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("day 31 in February = %v, want %v", got, want)
	}
}
