package commerce

import (
	"reflect"
	"testing"
)

func TestPlanAuthoringImageSourcesRepeatsSingleSourceToMeetRequestedCount(t *testing.T) {
	got := planAuthoringImageSources(3, []string{"source-1"}, nil)
	want := []string{"source-1", "source-1", "source-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planned sources = %v, want %v", got, want)
	}
}

func TestPlanAuthoringImageSourcesBalancesMultipleAssetsAndOnlyFillsMissingSlots(t *testing.T) {
	existing := []AuthoringImageCandidate{
		{SourceAssetID: "source-1", Status: ItemStatusProposed},
		{SourceAssetID: "source-1", Status: ItemStatusFailed},
	}
	got := planAuthoringImageSources(4, []string{"source-1", "source-2"}, existing)
	want := []string{"source-2", "source-1", "source-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planned sources = %v, want %v", got, want)
	}
}

func TestPlanAuthoringImageSourcesReturnsNoWorkWhenRequestIsSatisfiedOrInvalid(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		sources   []string
		existing  []AuthoringImageCandidate
	}{
		{name: "requested count already satisfied", requested: 2, sources: []string{"source-1"}, existing: []AuthoringImageCandidate{{Status: ItemStatusProposed}, {Status: ItemStatusAccepted}}},
		{name: "zero requested", requested: 0, sources: []string{"source-1"}},
		{name: "negative requested", requested: -1, sources: []string{"source-1"}},
		{name: "no sources", requested: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := planAuthoringImageSources(tt.requested, tt.sources, tt.existing); len(got) != 0 {
				t.Fatalf("planned sources = %v, want no work", got)
			}
		})
	}
}

func TestPlanAuthoringImageSourcesDoesNotCountFailedCandidatesAgainstRequestedCount(t *testing.T) {
	existing := []AuthoringImageCandidate{
		{SourceAssetID: "source-1", Status: ItemStatusFailed},
		{SourceAssetID: "source-1", Status: ItemStatusProposed},
	}
	got := planAuthoringImageSources(2, []string{"source-1"}, existing)
	want := []string{"source-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("planned sources = %v, want %v", got, want)
	}
}
