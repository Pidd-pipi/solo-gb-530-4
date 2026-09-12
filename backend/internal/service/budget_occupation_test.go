package service

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"radiation-dose-budget-control/backend/internal/constants"
	"radiation-dose-budget-control/backend/internal/dto"
	"radiation-dose-budget-control/backend/internal/model"
	"radiation-dose-budget-control/backend/internal/repository"
)

type occupationFixture struct {
	db          *gorm.DB
	planner     dto.Actor
	rpo         dto.Actor
	assessments *DoseBudgetAssessmentService
	plans       *WorkPermitPlanService
	occupations *BudgetOccupationService
}

func newOccupationFixture(t *testing.T, dsn string) occupationFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.WorkerProfile{}, &model.WorkPermitPlan{},
		&model.ExposureEntry{}, &model.DoseBudgetAssessment{}, &model.BudgetOccupation{}, &model.AuditEvent{},
	); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	plannerUser := model.User{Username: "planner", PasswordHash: "x", Role: constants.RolePlanner, Active: true}
	rpoUser := model.User{Username: "rpo", PasswordHash: "x", Role: constants.RoleRPOReviewer, Active: true}
	if err := db.Create(&plannerUser).Error; err != nil {
		t.Fatalf("create planner: %v", err)
	}
	if err := db.Create(&rpoUser).Error; err != nil {
		t.Fatalf("create rpo: %v", err)
	}
	workerRepo := repository.NewWorkerProfileRepository(db)
	planRepo := repository.NewWorkPermitPlanRepository(db)
	exposureRepo := repository.NewExposureEntryRepository(db)
	assessmentRepo := repository.NewDoseBudgetAssessmentRepository(db)
	occupationRepo := repository.NewBudgetOccupationRepository(db)
	systemRepo := repository.NewSystemRepository(db)
	audit := NewAuditService(systemRepo)
	occupationService := NewBudgetOccupationService(
		db, occupationRepo, planRepo, workerRepo, assessmentRepo, audit, 0.9, "ALARA-2026.1",
	)
	assessmentService := NewDoseBudgetAssessmentService(
		db, assessmentRepo, planRepo, workerRepo, exposureRepo, occupationService, audit, 0.9, "ALARA-2026.1",
	)
	planService := NewWorkPermitPlanService(db, planRepo, workerRepo, assessmentRepo, occupationService, audit)
	return occupationFixture{
		db:          db,
		planner:     dto.Actor{ID: plannerUser.ID, Username: "planner", Role: constants.RolePlanner},
		rpo:         dto.Actor{ID: rpoUser.ID, Username: "rpo", Role: constants.RoleRPOReviewer},
		assessments: assessmentService,
		plans:       planService,
		occupations: occupationService,
	}
}

func (fixture occupationFixture) seedWorkerPlan(t *testing.T, workerCode, planCode string, adminLimit, rate float64, minutes int, verifiedDose float64) (model.WorkerProfile, model.WorkPermitPlan) {
	t.Helper()
	now := time.Now().UTC()
	worker := model.WorkerProfile{
		WorkerCode: workerCode, DisplayName: "Ledger Test " + workerCode, AuthorizationLevel: "Controlled area L2",
		AnnualLimitMSV: 20, AdministrativeLimitMSV: adminLimit, ProfileStatus: constants.ProfileStatusActive,
		PeriodStart: now.Add(-30 * 24 * time.Hour), Version: 1,
	}
	if err := fixture.db.Create(&worker).Error; err != nil {
		t.Fatalf("create worker: %v", err)
	}
	plan := model.WorkPermitPlan{
		PlanCode: planCode, WorkerID: worker.ID, WorkArea: "Ledger bay", TaskCategory: "Ledger survey",
		EstimatedRateMSVH: rate, PlannedMinutes: minutes, ControlsJSON: `["temporary shielding"]`,
		PermitStatus: constants.PermitStatusDraft, Version: 1, CreatedBy: fixture.planner.ID,
	}
	if err := fixture.db.Create(&plan).Error; err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if verifiedDose > 0 {
		entry := model.ExposureEntry{
			WorkerID: worker.ID, SourceRef: "LEDGER-" + planCode, OccurredAt: now.Add(-2 * 24 * time.Hour),
			DoseMSV: verifiedDose, EntryType: constants.EntryTypeConfirmed, QualityFlag: constants.QualityFlagVerified,
			VerifiedBy: &fixture.rpo.ID, VerifiedAt: &now, Note: "verified ledger seed", CreatedBy: fixture.planner.ID,
		}
		if err := fixture.db.Create(&entry).Error; err != nil {
			t.Fatalf("create exposure: %v", err)
		}
	}
	return worker, plan
}

func (fixture occupationFixture) auditCount(action string) int64 {
	var count int64
	fixture.db.Model(&model.AuditEvent{}).Where("action = ?", action).Count(&count)
	return count
}

func (fixture occupationFixture) occupationCount() int64 {
	var count int64
	fixture.db.Model(&model.BudgetOccupation{}).Count(&count)
	return count
}

// TestOccupationRejectAndArchiveFlow drives submit -> reject -> archive and
// verifies occupy/release semantics, the single-view balance and audit.
func TestOccupationRejectAndArchiveFlow(t *testing.T) {
	fixture := newOccupationFixture(t, "file:occupation-reject-flow?mode=memory&cache=shared")
	worker, plan := fixture.seedWorkerPlan(t, "RP-LG-01", "LEDGER-A", 2, 2.8, 90, 1)

	assessment, err := fixture.assessments.Assess(dto.CreateDoseBudgetAssessmentRequest{
		PlanID: plan.ID, PeriodEnd: time.Now().UTC(), Version: 1,
	}, fixture.planner, "req-assess")
	if err != nil {
		t.Fatalf("assess: %v", err)
	}

	// Submission occupies 2.8 mSv/h × 90 min ÷ 60 = 4.2 mSv exactly once.
	submitted, err := fixture.assessments.Submit(assessment.ID, dto.PlanVersionRequest{Version: assessment.PlanVersion}, fixture.planner, "req-submit")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitted.AssessmentStatus != constants.AssessmentStatusSubmitted {
		t.Fatalf("assessment status = %q, want submitted", submitted.AssessmentStatus)
	}
	if fixture.occupationCount() != 1 {
		t.Fatalf("occupation rows = %d, want 1", fixture.occupationCount())
	}
	ledger, err := fixture.occupations.Get(1)
	if err != nil {
		t.Fatalf("get occupation: %v", err)
	}
	if ledger.OccupationStatus != constants.OccupationStatusOccupied || ledger.DoseMSV != 4.2 {
		t.Fatalf("occupation = %+v, want occupied 4.2 mSv", ledger)
	}
	if ledger.PeriodDoseMSV != 1 {
		t.Fatalf("period dose = %v, want 1", ledger.PeriodDoseMSV)
	}
	if fixture.auditCount("budget.occupied") != 1 {
		t.Fatal("expected one budget.occupied audit event")
	}

	// The same assessment can only occupy once: re-submission is rejected and
	// leaves exactly one ledger row.
	if _, err := fixture.assessments.Submit(assessment.ID, dto.PlanVersionRequest{Version: submitted.PlanVersion}, fixture.planner, "req-resubmit"); err == nil {
		t.Fatal("second submission unexpectedly succeeded")
	}
	if fixture.occupationCount() != 1 {
		t.Fatalf("occupation rows after duplicate submit = %d, want 1", fixture.occupationCount())
	}

	// Database-level guarantee: a second occupation row for the same
	// assessment violates the unique index.
	duplicate := model.BudgetOccupation{
		WorkerID: ledger.WorkerID, PlanID: ledger.PlanID, AssessmentID: ledger.AssessmentID,
		OccupationStatus: constants.OccupationStatusOccupied, DoseMSV: 1, PeriodDoseMSV: 1,
		OccupiedBy: fixture.planner.ID, OccupiedAt: time.Now().UTC(), Version: 1,
	}
	if err := fixture.db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate occupation for the same assessment was persisted")
	}

	// Single worker view: verified dose + active occupation + remaining quota.
	balances, _, err := fixture.occupations.WorkerBalances(1, 50, "")
	if err != nil {
		t.Fatalf("worker balances: %v", err)
	}
	var balance dto.WorkerBudgetBalanceResponse
	for _, candidate := range balances {
		if candidate.WorkerID == worker.ID {
			balance = candidate
		}
	}
	if balance.WorkerID == 0 {
		t.Fatal("worker balance not found")
	}
	if balance.VerifiedDoseMSV != 1 || balance.ActiveOccupationMSV != 4.2 || balance.CommittedDoseMSV != 5.2 {
		t.Fatalf("balance doses = verified %v active %v committed %v, want 1/4.2/5.2",
			balance.VerifiedDoseMSV, balance.ActiveOccupationMSV, balance.CommittedDoseMSV)
	}
	if balance.RiskBand != constants.DoseBandAboveAdmin || !balance.RequiresManualReview {
		t.Fatalf("over-limit occupation must flag manual review, band=%s review=%v", balance.RiskBand, balance.RequiresManualReview)
	}
	if !balance.PlanningOnly || balance.AutomaticWorkPermit {
		t.Fatal("balance view must stay planning-only and never emit a work permit")
	}
	if balance.RemainingAdminMSV != 0 {
		t.Fatalf("remaining admin = %v, expected clamped zero headroom above admin limit", balance.RemainingAdminMSV)
	}

	// RPO rejection releases the occupation in the same review transaction.
	rejected, err := fixture.assessments.Review(assessment.ID, dto.AssessmentReviewRequest{
		Decision: "reject", Note: "manual review: controls insufficient", Version: submitted.PlanVersion,
	}, fixture.rpo, "req-reject")
	if err != nil {
		t.Fatalf("review reject: %v", err)
	}
	if rejected.AssessmentStatus != constants.AssessmentStatusRejected {
		t.Fatalf("assessment status = %q, want rejected", rejected.AssessmentStatus)
	}
	ledger, err = fixture.occupations.Get(1)
	if err != nil {
		t.Fatalf("get occupation after reject: %v", err)
	}
	if ledger.OccupationStatus != constants.OccupationStatusReleased ||
		ledger.ReleaseReason != constants.OccupationReleaseReviewRejected || ledger.ReleasedBy == nil {
		t.Fatalf("occupation after reject = %+v, want released/review_rejected", ledger)
	}
	if fixture.auditCount("budget.released") != 1 {
		t.Fatal("expected one budget.released audit event after rejection")
	}

	// Archiving the rejected plan succeeds but does not double-release.
	if _, err := fixture.plans.Archive(plan.ID, dto.PlanVersionRequest{Version: rejected.PlanVersion}, fixture.planner, "req-archive-rejected"); err != nil {
		t.Fatalf("archive rejected plan: %v", err)
	}
	if fixture.auditCount("budget.released") != 1 {
		t.Fatal("rejected archival must not emit a second budget.released event")
	}
	ledger, _ = fixture.occupations.Get(1)
	if ledger.OccupationStatus != constants.OccupationStatusReleased {
		t.Fatalf("occupation status = %q, want released", ledger.OccupationStatus)
	}

	// Released budget is returned to the worker balance.
	balances, _, _ = fixture.occupations.WorkerBalances(1, 50, "")
	for _, candidate := range balances {
		if candidate.WorkerID == worker.ID {
			balance = candidate
		}
	}
	if balance.ActiveOccupationMSV != 0 || balance.ActiveOccupationCount != 0 || balance.CommittedDoseMSV != 1 {
		t.Fatalf("balance after release = active %v count %d committed %v, want 0/0/1",
			balance.ActiveOccupationMSV, balance.ActiveOccupationCount, balance.CommittedDoseMSV)
	}
}

// TestOccupationAcceptThenArchiveFlow verifies retention on acceptance and
// release on archival of an accepted plan.
func TestOccupationAcceptThenArchiveFlow(t *testing.T) {
	fixture := newOccupationFixture(t, "file:occupation-accept-flow?mode=memory&cache=shared")
	_, plan := fixture.seedWorkerPlan(t, "RP-LG-02", "LEDGER-B", 12, 0.42, 45, 0)

	assessment, err := fixture.assessments.Assess(dto.CreateDoseBudgetAssessmentRequest{
		PlanID: plan.ID, PeriodEnd: time.Now().UTC(), Version: 1,
	}, fixture.planner, "req-assess-2")
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	submitted, err := fixture.assessments.Submit(assessment.ID, dto.PlanVersionRequest{Version: assessment.PlanVersion}, fixture.planner, "req-submit-2")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	accepted, err := fixture.assessments.Review(assessment.ID, dto.AssessmentReviewRequest{
		Decision: "accept", Note: "planning evidence accepted by RPO", Version: submitted.PlanVersion,
	}, fixture.rpo, "req-accept")
	if err != nil {
		t.Fatalf("review accept: %v", err)
	}
	ledger, err := fixture.occupations.Get(1)
	if err != nil {
		t.Fatalf("get occupation: %v", err)
	}
	if ledger.OccupationStatus != constants.OccupationStatusRetained || ledger.RetainedBy == nil {
		t.Fatalf("occupation after accept = %+v, want retained", ledger)
	}
	if ledger.DoseMSV != 0.315 {
		t.Fatalf("retained dose = %v, want 0.315", ledger.DoseMSV)
	}
	if fixture.auditCount("budget.retained") != 1 {
		t.Fatal("expected one budget.retained audit event")
	}
	if fixture.auditCount("budget.released") != 0 {
		t.Fatal("acceptance must not release budget")
	}

	// Archiving the accepted plan releases the retained occupation.
	if _, err := fixture.plans.Archive(plan.ID, dto.PlanVersionRequest{Version: accepted.PlanVersion}, fixture.planner, "req-archive-accepted"); err != nil {
		t.Fatalf("archive accepted plan: %v", err)
	}
	ledger, _ = fixture.occupations.Get(1)
	if ledger.OccupationStatus != constants.OccupationStatusReleased ||
		ledger.ReleaseReason != constants.OccupationReleasePlanArchived {
		t.Fatalf("occupation after archive = %+v, want released/plan_archived", ledger)
	}
	if fixture.auditCount("budget.released") != 1 {
		t.Fatal("expected one budget.released audit event after accepted archival")
	}
}

// TestReviewRollsBackWhenOccupationMissing forces the occupation step of an
// acceptance to fail and verifies the assessment and plan states roll back
// together with no partial review audit.
func TestReviewRollsBackWhenOccupationMissing(t *testing.T) {
	fixture := newOccupationFixture(t, "file:occupation-rollback?mode=memory&cache=shared")
	_, plan := fixture.seedWorkerPlan(t, "RP-LG-03", "LEDGER-C", 12, 0.42, 45, 0)

	assessment, err := fixture.assessments.Assess(dto.CreateDoseBudgetAssessmentRequest{
		PlanID: plan.ID, PeriodEnd: time.Now().UTC(), Version: 1,
	}, fixture.planner, "req-assess-3")
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	submitted, err := fixture.assessments.Submit(assessment.ID, dto.PlanVersionRequest{Version: assessment.PlanVersion}, fixture.planner, "req-submit-3")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Simulate a divergent ledger: the occupation row vanished before review.
	if err := fixture.db.Where("assessment_id = ?", assessment.ID).Delete(&model.BudgetOccupation{}).Error; err != nil {
		t.Fatalf("delete occupation: %v", err)
	}
	if _, err := fixture.assessments.Review(assessment.ID, dto.AssessmentReviewRequest{
		Decision: "accept", Note: "acceptance must fail atomically", Version: submitted.PlanVersion,
	}, fixture.rpo, "req-accept-fail"); err == nil {
		t.Fatal("acceptance without occupation unexpectedly succeeded")
	}

	var stored model.DoseBudgetAssessment
	if err := fixture.db.First(&stored, assessment.ID).Error; err != nil {
		t.Fatalf("reload assessment: %v", err)
	}
	if stored.AssessmentStatus != constants.AssessmentStatusSubmitted {
		t.Fatalf("assessment status after failed review = %q, want submitted (rolled back)", stored.AssessmentStatus)
	}
	var storedPlan model.WorkPermitPlan
	if err := fixture.db.First(&storedPlan, plan.ID).Error; err != nil {
		t.Fatalf("reload plan: %v", err)
	}
	if storedPlan.PermitStatus != constants.PermitStatusPendingRPOReview {
		t.Fatalf("plan status after failed review = %q, want pending_rpo_review (rolled back)", storedPlan.PermitStatus)
	}
	if fixture.auditCount("assessment.reviewed") != 0 {
		t.Fatal("failed review must not leave an assessment.reviewed audit event")
	}
	if fixture.auditCount("budget.retained") != 0 {
		t.Fatal("failed review must not leave a budget.retained audit event")
	}
}

// TestArchiveRollsBackWhenAuditFails forces the archive transaction to fail at
// the audit step (audit table dropped) and verifies both the plan transition
// and the occupation release roll back together.
func TestArchiveRollsBackWhenAuditFails(t *testing.T) {
	fixture := newOccupationFixture(t, "file:occupation-archive-rollback?mode=memory&cache=shared")
	_, plan := fixture.seedWorkerPlan(t, "RP-LG-04", "LEDGER-D", 12, 0.42, 45, 0)

	assessment, err := fixture.assessments.Assess(dto.CreateDoseBudgetAssessmentRequest{
		PlanID: plan.ID, PeriodEnd: time.Now().UTC(), Version: 1,
	}, fixture.planner, "req-assess-4")
	if err != nil {
		t.Fatalf("assess: %v", err)
	}
	submitted, err := fixture.assessments.Submit(assessment.ID, dto.PlanVersionRequest{Version: assessment.PlanVersion}, fixture.planner, "req-submit-4")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	accepted, err := fixture.assessments.Review(assessment.ID, dto.AssessmentReviewRequest{
		Decision: "accept", Note: "accepted before broken archive", Version: submitted.PlanVersion,
	}, fixture.rpo, "req-accept-4")
	if err != nil {
		t.Fatalf("review accept: %v", err)
	}

	if err := fixture.db.Migrator().DropTable("audit_events"); err != nil {
		t.Fatalf("drop audit table: %v", err)
	}
	if _, err := fixture.plans.Archive(plan.ID, dto.PlanVersionRequest{Version: accepted.PlanVersion}, fixture.planner, "req-archive-fail"); err == nil {
		t.Fatal("archive unexpectedly succeeded after the audit table was dropped")
	}
	var storedPlan model.WorkPermitPlan
	if err := fixture.db.First(&storedPlan, plan.ID).Error; err != nil {
		t.Fatalf("reload plan: %v", err)
	}
	if storedPlan.PermitStatus != constants.PermitStatusPlanningAccepted {
		t.Fatalf("plan status after failed archive = %q, want planning_accepted (rolled back)", storedPlan.PermitStatus)
	}
	if storedPlan.Version != accepted.PlanVersion {
		t.Fatalf("plan version after failed archive = %d, want %d (rolled back)", storedPlan.Version, accepted.PlanVersion)
	}
	var occupation model.BudgetOccupation
	if err := fixture.db.Where("assessment_id = ?", assessment.ID).First(&occupation).Error; err != nil {
		t.Fatalf("reload occupation: %v", err)
	}
	if occupation.OccupationStatus != constants.OccupationStatusRetained {
		t.Fatalf("occupation after failed archive = %q, want retained (rolled back)", occupation.OccupationStatus)
	}
}
