package constants

import "testing"

func TestCanTransitionOccupation(t *testing.T) {
	cases := []struct {
		from string
		to   string
		want bool
	}{
		{OccupationStatusOccupied, OccupationStatusRetained, true},
		{OccupationStatusOccupied, OccupationStatusReleased, true},
		{OccupationStatusRetained, OccupationStatusReleased, true},
		{OccupationStatusReleased, OccupationStatusOccupied, false},
		{OccupationStatusReleased, OccupationStatusRetained, false},
		{OccupationStatusRetained, OccupationStatusOccupied, false},
		{OccupationStatusOccupied, OccupationStatusOccupied, false},
		{"", OccupationStatusReleased, false},
	}
	for _, tc := range cases {
		if got := CanTransitionOccupation(tc.from, tc.to); got != tc.want {
			t.Fatalf("CanTransitionOccupation(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestIsOccupationStatus(t *testing.T) {
	for _, value := range OccupationStatuses {
		if !IsOccupationStatus(value) {
			t.Fatalf("IsOccupationStatus(%q) = false, want true", value)
		}
	}
	if IsOccupationStatus("voided") {
		t.Fatal("voided is not a valid occupation status")
	}
}
