package main

import (
	"encoding/json"
	"testing"
)

func animeForTest(id int, title string, episodes int) MALAnime {
	return MALAnime{ID: id, Title: title, NumEpisodes: episodes, MediaType: "tv"}
}

func TestValidSplitPairAcceptsSameTitlePart2(t *testing.T) {
	first := animeForTest(1, "Tensei shitara Slime Datta Ken 2nd Season", 12)
	second := animeForTest(2, "Tensei shitara Slime Datta Ken 2nd Season Part 2", 12)
	if !validSplitPair(first, second) {
		t.Fatal("expected legitimate Part 2 pair to be accepted")
	}
}

func TestValidSplitPairRejectsUnrelatedPart2(t *testing.T) {
	cases := []struct{ first, second string }{
		{"Mushoku Tensei: Isekai Ittara Honki Dasu", "86 Part 2"},
		{"Kill la Kill", "Luv(sic) Part 2"},
	}
	for _, tc := range cases {
		if validSplitPair(animeForTest(1, tc.first, 12), animeForTest(2, tc.second, 12)) {
			t.Fatalf("unexpected split match: %q + %q", tc.first, tc.second)
		}
	}
}

func TestValidSplitPairPreservesSeasonIdentity(t *testing.T) {
	first := animeForTest(1, "Example 2nd Season", 12)
	second := animeForTest(2, "Example 3rd Season Part 2", 12)
	if validSplitPair(first, second) {
		t.Fatal("different seasons must not be paired")
	}
}

func TestIDStringAcceptsNumberAndString(t *testing.T) {
	for _, input := range []string{`{"media_id":123}`, `{"media_id":"anime-uuid-123"}`} {
		var item AVItem
		if err := json.Unmarshal([]byte(input), &item); err != nil {
			t.Fatalf("unmarshal %s: %v", input, err)
		}
		if item.MediaID == "" {
			t.Fatalf("empty media id for %s", input)
		}
	}
}
