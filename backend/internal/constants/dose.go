package constants

const (
	DoseBandWithinAdmin = "within_admin"
	DoseBandAboveAdmin  = "above_admin"
	DoseBandNearLegal   = "near_legal"
	DoseBandAboveLegal  = "above_legal"
	DoseBandInvalid     = "invalid"
)

var DoseBands = []string{
	DoseBandWithinAdmin,
	DoseBandAboveAdmin,
	DoseBandNearLegal,
	DoseBandAboveLegal,
	DoseBandInvalid,
}

const (
	AssessmentStatusCalculated = "calculated"
	AssessmentStatusSubmitted  = "submitted"
	AssessmentStatusAccepted   = "accepted"
	AssessmentStatusRejected   = "rejected"
)

const (
	EntryTypeConfirmed   = "confirmed"
	EntryTypeReversal    = "reversal"
	EntryTypeReplacement = "replacement"
)

const (
	QualityFlagPending  = "pending"
	QualityFlagVerified = "verified"
	QualityFlagRejected = "rejected"
)

func IsEntryType(value string) bool {
	return value == EntryTypeConfirmed || value == EntryTypeReversal || value == EntryTypeReplacement
}

func IsQualityFlag(value string) bool {
	return value == QualityFlagPending || value == QualityFlagVerified || value == QualityFlagRejected
}

// Budget occupation lifecycle: a submitted assessment occupies the planned
// increment, RPO acceptance retains it, rejection or plan archival releases it.
const (
	OccupationStatusOccupied = "occupied"
	OccupationStatusRetained = "retained"
	OccupationStatusReleased = "released"
)

// Release reasons are immutable evidence explaining why an occupation ended.
const (
	OccupationReleaseReviewRejected = "review_rejected"
	OccupationReleasePlanArchived   = "plan_archived"
)

var OccupationStatuses = []string{
	OccupationStatusOccupied,
	OccupationStatusRetained,
	OccupationStatusReleased,
}

func IsOccupationStatus(value string) bool {
	for _, candidate := range OccupationStatuses {
		if value == candidate {
			return true
		}
	}
	return false
}

// CanTransitionOccupation is the single source of truth for occupation state
// changes. Released is terminal.
func CanTransitionOccupation(from, to string) bool {
	allowed := map[string]map[string]bool{
		OccupationStatusOccupied: {OccupationStatusRetained: true, OccupationStatusReleased: true},
		OccupationStatusRetained: {OccupationStatusReleased: true},
	}
	return allowed[from][to]
}
