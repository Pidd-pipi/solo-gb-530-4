package model

import "time"

// BudgetOccupation records the planned dose that occupies a worker's period
// budget after an assessment is submitted for RPO review. Exactly one row can
// exist per assessment (uniqueIndex on assessment_id). Rows are never deleted:
// occupied -> retained on acceptance, occupied/retained -> released on
// rejection or archival.
type BudgetOccupation struct {
	ID               uint      `gorm:"primaryKey"`
	WorkerID         uint      `gorm:"index;not null"`
	PlanID           uint      `gorm:"index;not null"`
	AssessmentID     uint      `gorm:"uniqueIndex;not null"`
	OccupationStatus string    `gorm:"size:24;index;not null"`
	DoseMSV          float64   `gorm:"not null"`
	PeriodDoseMSV    float64   `gorm:"not null"`
	OccupiedBy       uint      `gorm:"index;not null"`
	OccupiedAt       time.Time `gorm:"index;not null"`
	RetainedBy       *uint     `gorm:"index"`
	RetainedAt       *time.Time
	ReleasedBy       *uint `gorm:"index"`
	ReleasedAt       *time.Time
	ReleaseReason    string    `gorm:"size:32;not null;default:''"`
	Version          uint      `gorm:"not null;default:1"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
}
