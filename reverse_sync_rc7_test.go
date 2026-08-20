package main

import "testing"

func TestAggregateSplitMAL(t *testing.T) {
	first := MALListItem{ID: 100, Title: "Season Part 1", Episodes: 13, Seen: 13, Status: "completed", AirStatus: "finished_airing"}
	second := MALListItem{ID: 101, Title: "Season Part 2", Episodes: 12, Seen: 12, Status: "completed", AirStatus: "finished_airing"}
	got := aggregateSplitMAL(first, second)
	if got.Seen != 25 || got.Episodes != 25 || got.Status != "completed" {
		t.Fatalf("unexpected aggregate: seen=%d episodes=%d status=%s", got.Seen, got.Episodes, got.Status)
	}
}

func TestAggregateSplitMALPartialIsWatching(t *testing.T) {
	first := MALListItem{ID: 100, Episodes: 12, Seen: 12, Status: "completed"}
	second := MALListItem{ID: 101, Episodes: 12, Seen: 0, Status: "plan_to_watch"}
	got := aggregateSplitMAL(first, second)
	if got.Seen != 12 || got.Status != "watching" {
		t.Fatalf("unexpected aggregate: seen=%d status=%s", got.Seen, got.Status)
	}
}

func TestCachedEntryForMALRejectsAmbiguousMappings(t *testing.T) {
	a := &App{cache: map[string]CacheEntry{
		"10": {MediaID: "10", MALID: 500},
		"11": {MediaID: "11", MALID: 500},
	}}
	if _, _, err := a.cachedEntryForMAL(500); err == nil {
		t.Fatal("expected ambiguous cache mapping error")
	}
}
