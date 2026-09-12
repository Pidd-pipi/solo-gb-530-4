package dosebudget

import "fmt"

// OccupationInput carries the immutable facts needed to turn one submitted
// assessment into one budget occupation.
type OccupationInput struct {
	WorkerID      uint
	PlanID        uint
	AssessmentID  uint
	DoseMSV       float64
	PeriodDoseMSV float64
}

// Validate checks that an occupation never carries non-finite or negative
// dose. A zero increment is allowed because genuine zero-dose planning
// scenarios still need their one-time occupation evidence row.
func (input OccupationInput) Validate() error {
	if input.WorkerID == 0 || input.PlanID == 0 || input.AssessmentID == 0 {
		return fmt.Errorf("%w: occupation requires worker, plan and assessment references", ErrInvalidDoseInput)
	}
	if !finite(input.DoseMSV) || input.DoseMSV < 0 {
		return fmt.Errorf("%w: occupied dose must be finite and non-negative", ErrInvalidDoseInput)
	}
	if !finite(input.PeriodDoseMSV) || input.PeriodDoseMSV < 0 {
		return fmt.Errorf("%w: period dose must be finite and non-negative", ErrInvalidDoseInput)
	}
	return nil
}

// PlannedDose is the single formula used when a submission occupies budget:
// estimated_rate_msvh × planned_minutes ÷ 60. It mirrors CalculateProjection
// but does not require a current period dose.
func PlannedDose(estimatedRateMSVH float64, plannedMinutes int) (float64, error) {
	projection, err := CalculateProjection(0, estimatedRateMSVH, plannedMinutes)
	if err != nil {
		return 0, err
	}
	return projection.PlannedDoseMSV, nil
}

// WorkerBudgetBalance is one worker's budget view: verified period dose,
// active occupations and remaining quota in a single structure.
type WorkerBudgetBalance struct {
	WorkerID               uint
	AdministrativeLimitMSV float64
	LegalLimitMSV          float64
	VerifiedDoseMSV        float64
	ActiveOccupationMSV    float64
	CommittedDoseMSV       float64
	RemainingAdminMSV      float64
	RemainingLegalMSV      float64
	ActiveOccupationCount  int
	RiskBand               string
	RequiresManualReview   bool
	EscalationExplanation  string
	PlanningOnly           bool
}

// BuildWorkerBalance assembles the single budget view for one worker.
// Over-limit values never block: the result only flags manual review and
// never represents an authorization to work.
func BuildWorkerBalance(
	workerID uint,
	administrativeLimitMSV, legalLimitMSV, verifiedDoseMSV, activeOccupationMSV float64,
	activeOccupationCount int,
	nearLegalRatio float64,
	thresholdVersion string,
) (WorkerBudgetBalance, error) {
	decision, err := Evaluate(verifiedDoseMSV+activeOccupationMSV, Thresholds{
		AdministrativeLimitMSV: administrativeLimitMSV,
		LegalLimitMSV:          legalLimitMSV,
		NearLegalRatio:         nearLegalRatio,
		Version:                thresholdVersion,
	})
	if err != nil {
		return WorkerBudgetBalance{}, err
	}
	committed := roundDose(verifiedDoseMSV + activeOccupationMSV)
	return WorkerBudgetBalance{
		WorkerID:               workerID,
		AdministrativeLimitMSV: administrativeLimitMSV,
		LegalLimitMSV:          legalLimitMSV,
		VerifiedDoseMSV:        roundDose(verifiedDoseMSV),
		ActiveOccupationMSV:    roundDose(activeOccupationMSV),
		CommittedDoseMSV:       committed,
		RemainingAdminMSV:      decision.RemainingAdminMSV,
		RemainingLegalMSV:      decision.RemainingLegalMSV,
		ActiveOccupationCount:  activeOccupationCount,
		RiskBand:               decision.RiskBand,
		RequiresManualReview:   decision.RequiresManualReview,
		EscalationExplanation:  decision.EscalationExplanation,
		PlanningOnly:           true,
	}, nil
}
