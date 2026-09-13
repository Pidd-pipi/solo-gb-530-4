package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"radiation-dose-budget-control/backend/internal/model"
)

type BudgetOccupationRepository struct{ db *gorm.DB }

func NewBudgetOccupationRepository(db *gorm.DB) *BudgetOccupationRepository {
	return &BudgetOccupationRepository{db: db}
}

func (repository *BudgetOccupationRepository) WithDB(db *gorm.DB) *BudgetOccupationRepository {
	return &BudgetOccupationRepository{db: db}
}

func (repository *BudgetOccupationRepository) Create(occupation *model.BudgetOccupation) error {
	if err := repository.db.Create(occupation).Error; err != nil {
		return fmt.Errorf("create budget occupation: %w", err)
	}
	return nil
}

func (repository *BudgetOccupationRepository) Find(id uint) (model.BudgetOccupation, error) {
	var occupation model.BudgetOccupation
	if err := repository.db.First(&occupation, id).Error; err != nil {
		return occupation, fmt.Errorf("find budget occupation: %w", err)
	}
	return occupation, nil
}

// FindByAssessment returns the occupation tied to one assessment. A missing
// row surfaces as gorm.ErrRecordNotFound so callers can distinguish
// "not occupied yet" from other failures.
func (repository *BudgetOccupationRepository) FindByAssessment(assessmentID uint) (model.BudgetOccupation, error) {
	var occupation model.BudgetOccupation
	if err := repository.db.Where("assessment_id = ?", assessmentID).First(&occupation).Error; err != nil {
		return occupation, fmt.Errorf("find assessment budget occupation: %w", err)
	}
	return occupation, nil
}

func (repository *BudgetOccupationRepository) FindForUpdate(id uint) (model.BudgetOccupation, error) {
	var occupation model.BudgetOccupation
	if err := repository.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&occupation, id).Error; err != nil {
		return occupation, fmt.Errorf("lock budget occupation: %w", err)
	}
	return occupation, nil
}

// FindByPlanForUpdate locks the occupation belonging to a plan. Used by the
// archival flow, which starts from the plan id.
func (repository *BudgetOccupationRepository) FindByPlanForUpdate(planID uint) (model.BudgetOccupation, error) {
	var occupation model.BudgetOccupation
	if err := repository.db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("plan_id = ?", planID).Order("id DESC").First(&occupation).Error; err != nil {
		return occupation, fmt.Errorf("lock plan budget occupation: %w", err)
	}
	return occupation, nil
}

func (repository *BudgetOccupationRepository) List(page, pageSize int, workerID, planID uint, status string) ([]model.BudgetOccupation, int64, error) {
	query := repository.db.Model(&model.BudgetOccupation{})
	if workerID > 0 {
		query = query.Where("worker_id = ?", workerID)
	}
	if planID > 0 {
		query = query.Where("plan_id = ?", planID)
	}
	if status != "" {
		query = query.Where("occupation_status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count budget occupations: %w", err)
	}
	var occupations []model.BudgetOccupation
	if err := query.Order("occupied_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&occupations).Error; err != nil {
		return nil, 0, fmt.Errorf("list budget occupations: %w", err)
	}
	return occupations, total, nil
}

// ActiveByWorker returns all non-released occupations for one worker, oldest
// first so the ledger reads chronologically.
func (repository *BudgetOccupationRepository) ActiveByWorker(workerID uint) ([]model.BudgetOccupation, error) {
	var occupations []model.BudgetOccupation
	if err := repository.db.Where("worker_id = ? AND occupation_status <> ?", workerID, "released").
		Order("occupied_at ASC, id ASC").Find(&occupations).Error; err != nil {
		return nil, fmt.Errorf("list active worker occupations: %w", err)
	}
	return occupations, nil
}

// Retain moves an occupied row to retained with a conditional update. The
// caller already holds the row lock via FindForUpdate; exactly one row must
// change.
func (repository *BudgetOccupationRepository) Retain(id uint, reviewerID uint, retainedAt time.Time) error {
	result := repository.db.Model(&model.BudgetOccupation{}).
		Where("id = ? AND occupation_status = ?", id, "occupied").
		Updates(map[string]any{
			"occupation_status": "retained", "retained_by": reviewerID, "retained_at": retainedAt,
			"version": gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return fmt.Errorf("retain budget occupation: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("retain budget occupation: %w", ErrStateConflict)
	}
	return nil
}

// Release moves occupied or retained rows to the terminal released state with
// a conditional update on the expected prior status.
func (repository *BudgetOccupationRepository) Release(id uint, fromStatus string, releasedBy uint, releasedAt time.Time, reason string) error {
	result := repository.db.Model(&model.BudgetOccupation{}).
		Where("id = ? AND occupation_status = ?", id, fromStatus).
		Updates(map[string]any{
			"occupation_status": "released", "released_by": releasedBy, "released_at": releasedAt,
			"release_reason": reason, "version": gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return fmt.Errorf("release budget occupation: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("release budget occupation: %w", ErrStateConflict)
	}
	return nil
}
