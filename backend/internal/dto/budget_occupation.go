package dto

import "time"

// BudgetOccupationResponse is one immutable occupation/release ledger row.
// PlanningOnly/AutomaticWorkPermit make the safety boundary explicit in the
// payload: the API can never answer "work is authorized".
type BudgetOccupationResponse struct {
	ID                  uint       `json:"id"`
	WorkerID            uint       `json:"worker_id"`
	WorkerCode          string     `json:"worker_code"`
	WorkerName          string     `json:"worker_name"`
	PlanID              uint       `json:"plan_id"`
	PlanCode            string     `json:"plan_code"`
	AssessmentID        uint       `json:"assessment_id"`
	OccupationStatus    string     `json:"occupation_status"`
	DoseMSV             float64    `json:"dose_msv"`
	PeriodDoseMSV       float64    `json:"period_dose_msv"`
	OccupiedBy          uint       `json:"occupied_by"`
	OccupiedAt          time.Time  `json:"occupied_at"`
	RetainedBy          *uint      `json:"retained_by,omitempty"`
	RetainedAt          *time.Time `json:"retained_at,omitempty"`
	ReleasedBy          *uint      `json:"released_by,omitempty"`
	ReleasedAt          *time.Time `json:"released_at,omitempty"`
	ReleaseReason       string     `json:"release_reason"`
	Version             uint       `json:"version"`
	PlanningOnly        bool       `json:"planning_only"`
	AutomaticWorkPermit bool       `json:"automatic_work_permit"`
}

// WorkerBudgetBalanceResponse is the single per-worker view combining verified
// dose, active occupation and remaining quota.
type WorkerBudgetBalanceResponse struct {
	WorkerID               uint      `json:"worker_id"`
	WorkerCode             string    `json:"worker_code"`
	WorkerName             string    `json:"worker_name"`
	AuthorizationLevel     string    `json:"authorization_level"`
	ProfileStatus          string    `json:"profile_status"`
	PeriodStart            time.Time `json:"period_start"`
	AdministrativeLimitMSV float64   `json:"administrative_limit_msv"`
	AnnualLegalLimitMSV    float64   `json:"annual_legal_limit_msv"`
	VerifiedDoseMSV        float64   `json:"verified_dose_msv"`
	ActiveOccupationMSV    float64   `json:"active_occupation_msv"`
	CommittedDoseMSV       float64   `json:"committed_dose_msv"`
	RemainingAdminMSV      float64   `json:"remaining_admin_msv"`
	RemainingLegalMSV      float64   `json:"remaining_legal_msv"`
	ActiveOccupationCount  int       `json:"active_occupation_count"`
	ActiveOccupationIDs    []uint    `json:"active_occupation_ids"`
	RiskBand               string    `json:"risk_band"`
	RequiresManualReview   bool      `json:"requires_manual_review"`
	EscalationExplanation  string    `json:"escalation_explanation"`
	ThresholdVersion       string    `json:"threshold_version"`
	PlanningOnly           bool      `json:"planning_only"`
	AutomaticWorkPermit    bool      `json:"automatic_work_permit"`
}
