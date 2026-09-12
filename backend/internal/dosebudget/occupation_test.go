package dosebudget

import (
	"math"
	"testing"

	"radiation-dose-budget-control/backend/internal/constants"
)

func TestPlannedDose(t *testing.T) {
	cases := []struct {
		name      string
		rate      float64
		minutes   int
		want      float64
		wantError bool
	}{
		{"forty five minutes", 0.42, 45, 0.315, false},
		{"ninety minutes", 2.8, 90, 4.2, false},
		{"zero rate is allowed", 0, 60, 0, false},
		{"negative rate rejected", -0.1, 60, 0, true},
		{"zero minutes rejected", 0.42, 0, 0, true},
		{"more than a day rejected", 0.42, 1441, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PlannedDose(tc.rate, tc.minutes)
			if tc.wantError {
				if err == nil {
					t.Fatalf("PlannedDose(%v, %d) expected error, got %v", tc.rate, tc.minutes, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlannedDose(%v, %d) unexpected error: %v", tc.rate, tc.minutes, err)
			}
			if math.Abs(got-tc.want) > 0.000001 {
				t.Fatalf("PlannedDose(%v, %d) = %v, want %v", tc.rate, tc.minutes, got, tc.want)
			}
		})
	}
}

func TestOccupationInputValidate(t *testing.T) {
	cases := []struct {
		name      string
		input     OccupationInput
		wantError bool
	}{
		{"valid", OccupationInput{WorkerID: 1, PlanID: 2, AssessmentID: 3, DoseMSV: 1.2, PeriodDoseMSV: 2.3}, false},
		{"zero dose allowed", OccupationInput{WorkerID: 1, PlanID: 2, AssessmentID: 3, DoseMSV: 0, PeriodDoseMSV: 0}, false},
		{"missing worker", OccupationInput{PlanID: 2, AssessmentID: 3, DoseMSV: 1}, true},
		{"missing plan", OccupationInput{WorkerID: 1, AssessmentID: 3, DoseMSV: 1}, true},
		{"missing assessment", OccupationInput{WorkerID: 1, PlanID: 2, DoseMSV: 1}, true},
		{"negative dose", OccupationInput{WorkerID: 1, PlanID: 2, AssessmentID: 3, DoseMSV: -0.1, PeriodDoseMSV: 0}, true},
		{"non finite dose", OccupationInput{WorkerID: 1, PlanID: 2, AssessmentID: 3, DoseMSV: math.NaN(), PeriodDoseMSV: 0}, true},
		{"negative period dose", OccupationInput{WorkerID: 1, PlanID: 2, AssessmentID: 3, DoseMSV: 1, PeriodDoseMSV: -0.1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if tc.wantError && err == nil {
				t.Fatalf("%s: expected validation error", tc.name)
			}
			if !tc.wantError && err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestBuildWorkerBalance(t *testing.T) {
	cases := []struct {
		name            string
		admin           float64
		legal           float64
		verified        float64
		active          float64
		wantBand        string
		wantReview      bool
		wantRemainAdm   float64
		wantRemainLegal float64
	}{
		{"within limits", 12, 20, 2, 3, constants.DoseBandWithinAdmin, false, 7, 15},
		{"above admin only flags review", 4, 20, 2, 3, constants.DoseBandAboveAdmin, true, 0, 15},
		{"near legal flags review", 12, 20, 17.5, 1, constants.DoseBandNearLegal, true, 0, 1.5},
		{"above legal flags review", 12, 20, 20.5, 0, constants.DoseBandAboveLegal, true, 0, 0},
		{"released occupation not counted", 12, 20, 2, 0, constants.DoseBandWithinAdmin, false, 10, 18},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			balance, err := BuildWorkerBalance(7, tc.admin, tc.legal, tc.verified, tc.active, 1, 0.9, "ALARA-2026.1")
			if err != nil {
				t.Fatalf("BuildWorkerBalance unexpected error: %v", err)
			}
			if balance.RiskBand != tc.wantBand {
				t.Fatalf("risk band = %q, want %q", balance.RiskBand, tc.wantBand)
			}
			if balance.RequiresManualReview != tc.wantReview {
				t.Fatalf("requires manual review = %v, want %v", balance.RequiresManualReview, tc.wantReview)
			}
			if math.Abs(balance.RemainingAdminMSV-tc.wantRemainAdm) > 0.000001 {
				t.Fatalf("remaining admin = %v, want %v", balance.RemainingAdminMSV, tc.wantRemainAdm)
			}
			if math.Abs(balance.RemainingLegalMSV-tc.wantRemainLegal) > 0.000001 {
				t.Fatalf("remaining legal = %v, want %v", balance.RemainingLegalMSV, tc.wantRemainLegal)
			}
			if balance.CommittedDoseMSV != roundDose(tc.verified+tc.active) {
				t.Fatalf("committed dose = %v, want %v", balance.CommittedDoseMSV, tc.verified+tc.active)
			}
			if !balance.PlanningOnly {
				t.Fatal("balance must stay planning-only evidence")
			}
		})
	}
}

func TestBuildWorkerBalanceRejectsBadThresholds(t *testing.T) {
	if _, err := BuildWorkerBalance(1, 20, 12, 1, 1, 1, 0.9, "ALARA-2026.1"); err == nil {
		t.Fatal("expected threshold ordering error when admin exceeds legal")
	}
	if _, err := BuildWorkerBalance(1, 12, 20, 1, 1, 1, 1.2, "ALARA-2026.1"); err == nil {
		t.Fatal("expected error for invalid near-legal ratio")
	}
}
