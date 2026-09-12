package service

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"radiation-dose-budget-control/backend/internal/constants"
	"radiation-dose-budget-control/backend/internal/dosebudget"
	"radiation-dose-budget-control/backend/internal/dto"
	"radiation-dose-budget-control/backend/internal/model"
	"radiation-dose-budget-control/backend/internal/repository"
)

type BudgetOccupationService struct {
	db               *gorm.DB
	occupations      *repository.BudgetOccupationRepository
	plans            *repository.WorkPermitPlanRepository
	workers          *repository.WorkerProfileRepository
	assessments      *repository.DoseBudgetAssessmentRepository
	audit            *AuditService
	nearRatio        float64
	thresholdVersion string
}

func NewBudgetOccupationService(
	db *gorm.DB,
	occupations *repository.BudgetOccupationRepository,
	plans *repository.WorkPermitPlanRepository,
	workers *repository.WorkerProfileRepository,
	assessments *repository.DoseBudgetAssessmentRepository,
	audit *AuditService,
	nearRatio float64,
	thresholdVersion string,
) *BudgetOccupationService {
	return &BudgetOccupationService{
		db: db, occupations: occupations, plans: plans, workers: workers, assessments: assessments,
		audit: audit, nearRatio: nearRatio, thresholdVersion: thresholdVersion,
	}
}

// OccupyOnSubmit creates the single occupation for a submitted assessment. It
// runs inside the caller's submission transaction and is idempotent at the
// database boundary: one occupation per assessment, enforced by a unique index.
func (service *BudgetOccupationService) OccupyOnSubmit(
	tx *gorm.DB,
	assessment model.DoseBudgetAssessment,
	plan model.WorkPermitPlan,
	worker model.WorkerProfile,
	actor dto.Actor,
	requestID string,
) (model.BudgetOccupation, error) {
	occupations := service.occupations.WithDB(tx)
	if _, err := occupations.FindByAssessment(assessment.ID); err == nil {
		return model.BudgetOccupation{}, Conflict("occupation_exists", "this assessment already occupies budget once", nil)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.BudgetOccupation{}, Internal("could not verify occupation uniqueness", err)
	}
	dose, err := dosebudget.PlannedDose(plan.EstimatedRateMSVH, plan.PlannedMinutes)
	if err != nil {
		return model.BudgetOccupation{}, BadRequest("invalid_projection", err.Error())
	}
	input := dosebudget.OccupationInput{
		WorkerID: plan.WorkerID, PlanID: plan.ID, AssessmentID: assessment.ID,
		DoseMSV: dose, PeriodDoseMSV: assessment.PeriodDoseMSV,
	}
	if err := input.Validate(); err != nil {
		return model.BudgetOccupation{}, BadRequest("invalid_occupation", err.Error())
	}
	now := time.Now().UTC()
	occupation := model.BudgetOccupation{
		WorkerID: input.WorkerID, PlanID: input.PlanID, AssessmentID: input.AssessmentID,
		OccupationStatus: constants.OccupationStatusOccupied, DoseMSV: input.DoseMSV,
		PeriodDoseMSV: input.PeriodDoseMSV, OccupiedBy: actor.ID, OccupiedAt: now, Version: 1,
	}
	if err := occupations.Create(&occupation); err != nil {
		if repository.IsUniqueViolation(err) {
			return model.BudgetOccupation{}, Conflict("occupation_exists", "this assessment already occupies budget once", err)
		}
		return model.BudgetOccupation{}, Internal("could not create budget occupation", err)
	}
	// Over-limit scenarios only flag human review: occupation is still
	// recorded and no work permit or authorization is produced.
	balance, err := dosebudget.BuildWorkerBalance(
		input.WorkerID, worker.AdministrativeLimitMSV, worker.AnnualLimitMSV,
		input.PeriodDoseMSV, input.DoseMSV, 1, service.nearRatio, service.thresholdVersion,
	)
	if err != nil {
		return model.BudgetOccupation{}, BadRequest("invalid_thresholds", err.Error())
	}
	if err := service.audit.RecordTx(tx, actor, requestID, "budget.occupied", "budget_occupation", auditID(occupation.ID),
		map[string]any{
			"assessment_id": assessment.ID, "plan_id": plan.ID, "worker_id": plan.WorkerID,
			"occupied_dose_msv": occupation.DoseMSV, "period_dose_msv": occupation.PeriodDoseMSV,
			"risk_band": balance.RiskBand, "requires_manual_review": balance.RequiresManualReview,
			"planning_only": true, "automatic_work_permit": false,
		}, nil, occupationAudit(occupation)); err != nil {
		return model.BudgetOccupation{}, err
	}
	return occupation, nil
}

// RetainOnAccept keeps the occupation after an RPO acceptance. Caller already
// holds the plan and assessment locks inside its transaction.
func (service *BudgetOccupationService) RetainOnAccept(
	tx *gorm.DB,
	assessmentID uint,
	actor dto.Actor,
	requestID string,
) (model.BudgetOccupation, error) {
	occupations := service.occupations.WithDB(tx)
	occupation, err := occupations.FindByAssessment(assessmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.BudgetOccupation{}, Conflict("occupation_missing", "accepted assessment has no budget occupation", err)
		}
		return model.BudgetOccupation{}, Internal("could not load budget occupation", err)
	}
	before := occupation
	if occupation.OccupationStatus != constants.OccupationStatusOccupied {
		return model.BudgetOccupation{}, Conflict("invalid_state", "only occupied budgets can be retained", nil)
	}
	if !constants.CanTransitionOccupation(occupation.OccupationStatus, constants.OccupationStatusRetained) {
		return model.BudgetOccupation{}, Conflict("invalid_state", "occupation retain transition is not allowed", nil)
	}
	now := time.Now().UTC()
	if err := occupations.Retain(occupation.ID, actor.ID, now); err != nil {
		return model.BudgetOccupation{}, Conflict("state_conflict", "occupation changed before retention", err)
	}
	occupation.OccupationStatus = constants.OccupationStatusRetained
	occupation.RetainedBy = &actor.ID
	occupation.RetainedAt = &now
	occupation.Version++
	if err := service.audit.RecordTx(tx, actor, requestID, "budget.retained", "budget_occupation", auditID(occupation.ID),
		map[string]any{
			"assessment_id": assessmentID, "retained_dose_msv": occupation.DoseMSV,
			"planning_acceptance_is_not_work_permit": true,
		}, occupationAudit(before), occupationAudit(occupation)); err != nil {
		return model.BudgetOccupation{}, err
	}
	return occupation, nil
}

// ReleaseOnReject releases the occupation when the RPO rejects the assessment.
func (service *BudgetOccupationService) ReleaseOnReject(
	tx *gorm.DB,
	assessmentID uint,
	reason string,
	actor dto.Actor,
	requestID string,
) (model.BudgetOccupation, error) {
	return service.release(tx, assessmentID, reason, actor, requestID, "budget.released")
}

// ReleaseOnArchive releases a still-active occupation when the reviewed plan
// is archived. Accepted plans hold retained occupations; rejected plans were
// already released and archive without a second release event.
func (service *BudgetOccupationService) ReleaseOnArchive(
	tx *gorm.DB,
	planID uint,
	actor dto.Actor,
	requestID string,
) (model.BudgetOccupation, bool, error) {
	occupations := service.occupations.WithDB(tx)
	occupation, err := occupations.FindByPlanForUpdate(planID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.BudgetOccupation{}, false, nil
		}
		return model.BudgetOccupation{}, false, Internal("could not load plan budget occupation", err)
	}
	if occupation.OccupationStatus == constants.OccupationStatusReleased {
		return occupation, false, nil
	}
	released, err := service.applyRelease(tx, occupation, constants.OccupationReleasePlanArchived, actor, requestID, "budget.released")
	if err != nil {
		return model.BudgetOccupation{}, false, err
	}
	return released, true, nil
}

func (service *BudgetOccupationService) release(
	tx *gorm.DB,
	assessmentID uint,
	reason string,
	actor dto.Actor,
	requestID string,
	action string,
) (model.BudgetOccupation, error) {
	occupations := service.occupations.WithDB(tx)
	occupation, err := occupations.FindByAssessment(assessmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.BudgetOccupation{}, Conflict("occupation_missing", "reviewed assessment has no budget occupation", err)
		}
		return model.BudgetOccupation{}, Internal("could not load budget occupation", err)
	}
	return service.applyRelease(tx, occupation, reason, actor, requestID, action)
}

func (service *BudgetOccupationService) applyRelease(
	tx *gorm.DB,
	occupation model.BudgetOccupation,
	reason string,
	actor dto.Actor,
	requestID string,
	action string,
) (model.BudgetOccupation, error) {
	occupations := service.occupations.WithDB(tx)
	before := occupation
	if occupation.OccupationStatus == constants.OccupationStatusReleased {
		return model.BudgetOccupation{}, Conflict("invalid_state", "budget occupation is already released", nil)
	}
	if !constants.CanTransitionOccupation(occupation.OccupationStatus, constants.OccupationStatusReleased) {
		return model.BudgetOccupation{}, Conflict("invalid_state", "occupation release transition is not allowed", nil)
	}
	now := time.Now().UTC()
	if err := occupations.Release(occupation.ID, occupation.OccupationStatus, actor.ID, now, reason); err != nil {
		return model.BudgetOccupation{}, Conflict("state_conflict", "occupation changed before release", err)
	}
	occupation.OccupationStatus = constants.OccupationStatusReleased
	occupation.ReleasedBy = &actor.ID
	occupation.ReleasedAt = &now
	occupation.ReleaseReason = reason
	occupation.Version++
	if err := service.audit.RecordTx(tx, actor, requestID, action, "budget_occupation", auditID(occupation.ID),
		map[string]any{
			"assessment_id": occupation.AssessmentID, "plan_id": occupation.PlanID, "worker_id": occupation.WorkerID,
			"released_dose_msv": occupation.DoseMSV, "release_reason": reason,
		}, occupationAudit(before), occupationAudit(occupation)); err != nil {
		return model.BudgetOccupation{}, err
	}
	return occupation, nil
}

func (service *BudgetOccupationService) Get(id uint) (dto.BudgetOccupationResponse, error) {
	occupation, err := service.occupations.Find(id)
	if err != nil {
		return dto.BudgetOccupationResponse{}, MapRepositoryError("budget occupation", err)
	}
	return service.respond(occupation)
}

func (service *BudgetOccupationService) List(
	page, pageSize int,
	workerFilter, planFilter, status string,
) ([]dto.BudgetOccupationResponse, dto.PageMeta, error) {
	if status != "" && !constants.IsOccupationStatus(status) {
		return nil, dto.PageMeta{}, BadRequest("invalid_occupation_status", "occupation_status filter is not recognized")
	}
	workerID, err := parseUintFilter(workerFilter)
	if err != nil {
		return nil, dto.PageMeta{}, err
	}
	planID, err := parseUintFilter(planFilter)
	if err != nil {
		return nil, dto.PageMeta{}, err
	}
	occupations, total, err := service.occupations.List(page, pageSize, workerID, planID, status)
	if err != nil {
		return nil, dto.PageMeta{}, Internal("could not list budget occupations", err)
	}
	responses := make([]dto.BudgetOccupationResponse, 0, len(occupations))
	for _, occupation := range occupations {
		response, err := service.respond(occupation)
		if err != nil {
			return nil, dto.PageMeta{}, err
		}
		responses = append(responses, response)
	}
	return responses, pageMeta(page, pageSize, total), nil
}

// WorkerBalances builds the single budget view for every active worker:
// verified period dose, active occupation and remaining quota together.
func (service *BudgetOccupationService) WorkerBalances(page, pageSize int, status string) ([]dto.WorkerBudgetBalanceResponse, dto.PageMeta, error) {
	if status == "" {
		status = constants.ProfileStatusActive
	}
	if !constants.IsProfileStatus(status) {
		return nil, dto.PageMeta{}, BadRequest("invalid_profile_status", "profile_status filter is not recognized")
	}
	workers, total, err := service.workers.List(page, pageSize, status, "")
	if err != nil {
		return nil, dto.PageMeta{}, Internal("could not list worker profiles", err)
	}
	workerIDs := make([]uint, 0, len(workers))
	for _, worker := range workers {
		workerIDs = append(workerIDs, worker.ID)
	}
	activeByWorker, err := service.occupations.ActiveByWorkers(workerIDs)
	if err != nil {
		return nil, dto.PageMeta{}, Internal("could not summarize active occupations", err)
	}
	now := time.Now().UTC()
	responses := make([]dto.WorkerBudgetBalanceResponse, 0, len(workers))
	for _, worker := range workers {
		period, err := dosebudget.NewPeriod(worker.PeriodStart, now)
		if err != nil {
			return nil, dto.PageMeta{}, BadRequest("invalid_period", err.Error())
		}
		verifiedDose, err := service.workers.PeriodDose(worker.ID, period.Start, period.End)
		if err != nil {
			return nil, dto.PageMeta{}, Internal("could not summarize verified dose", err)
		}
		active := activeByWorker[worker.ID]
		var activeDose float64
		activeIDs := make([]uint, 0, len(active))
		for _, occupation := range active {
			activeDose += occupation.DoseMSV
			activeIDs = append(activeIDs, occupation.ID)
		}
		balance, err := dosebudget.BuildWorkerBalance(
			worker.ID, worker.AdministrativeLimitMSV, worker.AnnualLimitMSV,
			verifiedDose, activeDose, len(active), service.nearRatio, service.thresholdVersion,
		)
		if err != nil {
			return nil, dto.PageMeta{}, BadRequest("invalid_thresholds", err.Error())
		}
		responses = append(responses, dto.WorkerBudgetBalanceResponse{
			WorkerID: worker.ID, WorkerCode: worker.WorkerCode, WorkerName: worker.DisplayName,
			AuthorizationLevel: worker.AuthorizationLevel, ProfileStatus: worker.ProfileStatus,
			PeriodStart: worker.PeriodStart, AdministrativeLimitMSV: balance.AdministrativeLimitMSV,
			AnnualLegalLimitMSV: balance.LegalLimitMSV, VerifiedDoseMSV: balance.VerifiedDoseMSV,
			ActiveOccupationMSV: balance.ActiveOccupationMSV, CommittedDoseMSV: balance.CommittedDoseMSV,
			RemainingAdminMSV: balance.RemainingAdminMSV, RemainingLegalMSV: balance.RemainingLegalMSV,
			ActiveOccupationCount: balance.ActiveOccupationCount, ActiveOccupationIDs: activeIDs,
			RiskBand: balance.RiskBand, RequiresManualReview: balance.RequiresManualReview,
			EscalationExplanation: balance.EscalationExplanation,
			ThresholdVersion:      service.thresholdVersion, PlanningOnly: true,
			AutomaticWorkPermit: false,
		})
	}
	return responses, pageMeta(page, pageSize, total), nil
}

func (service *BudgetOccupationService) respond(occupation model.BudgetOccupation) (dto.BudgetOccupationResponse, error) {
	worker, err := service.workers.Find(occupation.WorkerID)
	if err != nil {
		return dto.BudgetOccupationResponse{}, MapRepositoryError("occupation worker", err)
	}
	plan, err := service.plans.Find(occupation.PlanID)
	if err != nil {
		return dto.BudgetOccupationResponse{}, MapRepositoryError("occupation plan", err)
	}
	return dto.BudgetOccupationResponse{
		ID: occupation.ID, WorkerID: occupation.WorkerID, WorkerCode: worker.WorkerCode, WorkerName: worker.DisplayName,
		PlanID: occupation.PlanID, PlanCode: plan.PlanCode, AssessmentID: occupation.AssessmentID,
		OccupationStatus: occupation.OccupationStatus, DoseMSV: occupation.DoseMSV,
		PeriodDoseMSV: occupation.PeriodDoseMSV, OccupiedBy: occupation.OccupiedBy, OccupiedAt: occupation.OccupiedAt,
		RetainedBy: occupation.RetainedBy, RetainedAt: occupation.RetainedAt,
		ReleasedBy: occupation.ReleasedBy, ReleasedAt: occupation.ReleasedAt,
		ReleaseReason: occupation.ReleaseReason, Version: occupation.Version,
		PlanningOnly: true, AutomaticWorkPermit: false,
	}, nil
}

func occupationAudit(occupation model.BudgetOccupation) map[string]any {
	return map[string]any{
		"id": occupation.ID, "worker_id": occupation.WorkerID, "plan_id": occupation.PlanID,
		"assessment_id": occupation.AssessmentID, "occupation_status": occupation.OccupationStatus,
		"dose_msv": occupation.DoseMSV, "period_dose_msv": occupation.PeriodDoseMSV,
		"release_reason": occupation.ReleaseReason, "version": occupation.Version,
	}
}
