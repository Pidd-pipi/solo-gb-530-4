package service

import (
	"testing"
	"time"

	"radiation-dose-budget-control/backend/internal/constants"
	"radiation-dose-budget-control/backend/internal/dto"
	"radiation-dose-budget-control/backend/internal/model"
)

// This file groups the repeatable failure-path and boundary regression tests
// for the occupation ledger. Every test uses its own in-memory SQLite DSN so
// the suite is order-independent and stable under repeated runs.

// review submits the assessment for an already calculated plan, then records
// an RPO accept/reject decision.
func (fixture occupationFixture) review(t *testing.T, submitted dto.DoseBudgetAssessmentResponse, decision, note, requestID string) dto.DoseBudgetAssessmentResponse {
	t.Helper()
	reviewed, err := fixture.assessments.Review(submitted.ID, dto.AssessmentReviewRequest{
		Decision: decision, Note: note, Version: submitted.PlanVersion,
	}, fixture.rpo, requestID)
	if err != nil {
		t.Fatalf("review %s: %v", decision, err)
	}
	return reviewed
}

// assertBalance loads the worker balance view and asserts the occupation
// aggregate plus cumulative risk band.
func (fixture occupationFixture) assertBalance(t *testing.T, workerID uint, wantActive float64, wantCount int, wantCommitted float64, wantBand string) {
	t.Helper()
	balances, _, err := fixture.occupations.WorkerBalances(1, 50, "")
	if err != nil {
		t.Fatalf("worker balances: %v", err)
	}
	view := balanceForWorker(balances, workerID)
	if view.WorkerID == 0 {
		t.Fatalf("balance for worker %d not found", workerID)
	}
	if view.ActiveOccupationMSV != wantActive || view.ActiveOccupationCount != wantCount ||
		view.CommittedDoseMSV != wantCommitted || view.RiskBand != wantBand {
		t.Fatalf("balance = active %v count %d committed %v band %s, want active %v count %d committed %v band %s",
			view.ActiveOccupationMSV, view.ActiveOccupationCount, view.CommittedDoseMSV, view.RiskBand,
			wantActive, wantCount, wantCommitted, wantBand)
	}
}

// TestSequentialSubmissionsCumulativeRisk submits three plans for one worker
// one after another without releasing, and verifies that each fresh
// occupation's audit band tracks the cumulative active budget:
// 0.8 within_admin, 1.6 within_admin, 2.4 above_admin — even though every
// individual increment (0.8) is within the 2 mSv administrative limit.
func TestSequentialSubmissionsCumulativeRisk(t *testing.T) {
	fixture := newOccupationFixture(t, "file:occupation-sequential?mode=memory&cache=shared")
	worker, planA := fixture.seedWorkerPlan(t, "RP-SEQ-01", "SEQ-A", 2, 0.8, 60, 0)
	planB := fixture.seedPlan(t, worker.ID, "SEQ-B", 0.8, 60)
	planC := fixture.seedPlan(t, worker.ID, "SEQ-C", 0.8, 60)

	steps := []struct {
		plan          model.WorkPermitPlan
		requestID     string
		wantBand      string
		wantReview    bool
		wantActive    float64
		wantCount     int
		wantCommitted float64
	}{
		{planA, "req-seq-a", constants.DoseBandWithinAdmin, false, 0.8, 1, 0.8},
		{planB, "req-seq-b", constants.DoseBandWithinAdmin, false, 1.6, 2, 1.6},
		{planC, "req-seq-c", constants.DoseBandAboveAdmin, true, 2.4, 3, 2.4},
	}
	for index, step := range steps {
		submitted := fixture.assessAndSubmit(t, step.plan, step.requestID)
		occupation := fixture.occupationByAssessment(t, submitted.ID)
		audit := occupationAuditParameters(t, fixture.db, occupation.ID, "budget.occupied")
		if audit["risk_band"] != step.wantBand || audit["requires_manual_review"] != step.wantReview {
			t.Fatalf("step %d audit = band %v review %v, want %s/%v",
				index, audit["risk_band"], audit["requires_manual_review"], step.wantBand, step.wantReview)
		}
		if audit["active_occupation_msv"] != step.wantActive || audit["active_occupation_count"] != float64(step.wantCount) ||
			audit["committed_dose_msv"] != step.wantCommitted {
			t.Fatalf("step %d audit aggregates = active %v count %v committed %v, want %v/%d/%v",
				index, audit["active_occupation_msv"], audit["active_occupation_count"], audit["committed_dose_msv"],
				step.wantActive, step.wantCount, step.wantCommitted)
		}
		fixture.assertBalance(t, worker.ID, step.wantActive, step.wantCount, step.wantCommitted, step.wantBand)
	}
}

// TestResubmitAfterRejectRelease verifies that after an occupation is released
// by RPO rejection it no longer counts, and a brand-new plan submitted for the
// same worker is evaluated against only the remaining active occupations.
func TestResubmitAfterRejectRelease(t *testing.T) {
	fixture := newOccupationFixture(t, "file:occupation-after-reject?mode=memory&cache=shared")
	worker, planA := fixture.seedWorkerPlan(t, "RP-RR-01", "RR-A", 2, 1.5, 60, 0)
	planB := fixture.seedPlan(t, worker.ID, "RR-B", 1.5, 60)

	// First submission occupies 1.5 and stays within the admin limit.
	submittedA := fixture.assessAndSubmit(t, planA, "req-rr-a")
	fixture.assertBalance(t, worker.ID, 1.5, 1, 1.5, constants.DoseBandWithinAdmin)

	// RPO rejection releases the occupation in the same transaction.
	fixture.review(t, submittedA, "reject", "release occupation before resubmit", "req-rr-a-reject")
	fixture.assertBalance(t, worker.ID, 0, 0, 0, constants.DoseBandWithinAdmin)
	released := fixture.occupationByAssessment(t, submittedA.ID)
	if released.OccupationStatus != constants.OccupationStatusReleased ||
		released.ReleaseReason != constants.OccupationReleaseReviewRejected {
		t.Fatalf("occupation A = %+v, want released/review_rejected", released)
	}

	// A new plan after the release occupies 1.5 again and must not resurrect or
	// double-count the released row.
	submittedB := fixture.assessAndSubmit(t, planB, "req-rr-b")
	occupationB := fixture.occupationByAssessment(t, submittedB.ID)
	auditB := occupationAuditParameters(t, fixture.db, occupationB.ID, "budget.occupied")
	if auditB["risk_band"] != constants.DoseBandWithinAdmin || auditB["requires_manual_review"] != false {
		t.Fatalf("post-release audit = band %v review %v, want within_admin/false", auditB["risk_band"], auditB["requires_manual_review"])
	}
	if auditB["active_occupation_msv"] != 1.5 || auditB["active_occupation_count"] != float64(1) {
		t.Fatalf("post-release audit aggregates = %+v, want only the new 1.5 mSv occupation", auditB)
	}
	fixture.assertBalance(t, worker.ID, 1.5, 1, 1.5, constants.DoseBandWithinAdmin)

	// Ledger history is preserved: exactly one released and one occupied row.
	var statuses []model.BudgetOccupation
	if err := fixture.db.Where("worker_id = ?", worker.ID).Order("id ASC").Find(&statuses).Error; err != nil {
		t.Fatalf("load occupation history: %v", err)
	}
	if len(statuses) != 2 || statuses[0].OccupationStatus != constants.OccupationStatusReleased ||
		statuses[1].OccupationStatus != constants.OccupationStatusOccupied {
		t.Fatalf("occupation history = %+v, want released then occupied", statuses)
	}
}

// TestResubmitAfterArchiveRelease accepts a plan, archives it (releasing the
// retained occupation), then submits a fresh plan and verifies the released
// retained row is excluded from the new cumulative calculation.
func TestResubmitAfterArchiveRelease(t *testing.T) {
	fixture := newOccupationFixture(t, "file:occupation-after-archive?mode=memory&cache=shared")
	worker, planA := fixture.seedWorkerPlan(t, "RP-AR-01", "AR-A", 2, 1.5, 60, 0)
	planB := fixture.seedPlan(t, worker.ID, "AR-B", 1.5, 60)

	submittedA := fixture.assessAndSubmit(t, planA, "req-ar-a")
	acceptedA := fixture.review(t, submittedA, "accept", "planning evidence retained", "req-ar-a-accept")
	if acceptedA.AssessmentStatus != constants.AssessmentStatusAccepted {
		t.Fatalf("assessment A status = %s, want accepted", acceptedA.AssessmentStatus)
	}
	fixture.assertBalance(t, worker.ID, 1.5, 1, 1.5, constants.DoseBandWithinAdmin)

	// Archive the accepted plan; its retained occupation is released.
	if _, err := fixture.plans.Archive(planA.ID, dto.PlanVersionRequest{Version: acceptedA.PlanVersion}, fixture.planner, "req-ar-a-archive"); err != nil {
		t.Fatalf("archive accepted plan: %v", err)
	}
	fixture.assertBalance(t, worker.ID, 0, 0, 0, constants.DoseBandWithinAdmin)
	archived := fixture.occupationByAssessment(t, submittedA.ID)
	if archived.OccupationStatus != constants.OccupationStatusReleased ||
		archived.ReleaseReason != constants.OccupationReleasePlanArchived {
		t.Fatalf("occupation A after archive = %+v, want released/plan_archived", archived)
	}

	// A new plan submitted after archival counts only itself; the retained-then-
	// released occupation must not linger in the cumulative budget.
	submittedB := fixture.assessAndSubmit(t, planB, "req-ar-b")
	occupationB := fixture.occupationByAssessment(t, submittedB.ID)
	auditB := occupationAuditParameters(t, fixture.db, occupationB.ID, "budget.occupied")
	if auditB["risk_band"] != constants.DoseBandWithinAdmin || auditB["active_occupation_count"] != float64(1) ||
		auditB["active_occupation_msv"] != 1.5 {
		t.Fatalf("post-archive audit = %+v, want within_admin with only the new occupation", auditB)
	}
	fixture.assertBalance(t, worker.ID, 1.5, 1, 1.5, constants.DoseBandWithinAdmin)
}

// TestSubmitRollsBackWhenAuditWriteFails drops the audit table so the
// budget.occupied write inside the submission transaction fails. The occupation
// row, the assessment transition and the plan transition must all roll back
// together; restoring the table allows the same assessment to submit cleanly.
func TestSubmitRollsBackWhenAuditWriteFails(t *testing.T) {
	fixture := newOccupationFixture(t, "file:occupation-submit-rollback?mode=memory&cache=shared")
	worker, plan := fixture.seedWorkerPlan(t, "RP-RB-01", "RB-A", 12, 0.42, 45, 0)
	assessed, err := fixture.assessments.Assess(dto.CreateDoseBudgetAssessmentRequest{
		PlanID: plan.ID, PeriodEnd: time.Now().UTC(), Version: 1,
	}, fixture.planner, "req-rb-assess")
	if err != nil {
		t.Fatalf("assess: %v", err)
	}

	if err := fixture.db.Migrator().DropTable("audit_events"); err != nil {
		t.Fatalf("drop audit table: %v", err)
	}
	if _, err := fixture.assessments.Submit(assessed.ID, dto.PlanVersionRequest{Version: assessed.PlanVersion}, fixture.planner, "req-rb-submit-fail"); err == nil {
		t.Fatal("submission unexpectedly succeeded with the audit table unavailable")
	}

	// Occupation row was rolled back.
	var occupationCount int64
	if err := fixture.db.Model(&model.BudgetOccupation{}).Where("assessment_id = ?", assessed.ID).Count(&occupationCount).Error; err != nil {
		t.Fatalf("count occupations: %v", err)
	}
	if occupationCount != 0 {
		t.Fatalf("occupation rows after failed submit = %d, want 0 (rolled back)", occupationCount)
	}

	// Assessment and plan state were both rolled back.
	var stored model.DoseBudgetAssessment
	if err := fixture.db.First(&stored, assessed.ID).Error; err != nil {
		t.Fatalf("reload assessment: %v", err)
	}
	if stored.AssessmentStatus != constants.AssessmentStatusCalculated {
		t.Fatalf("assessment status = %q, want calculated (rolled back)", stored.AssessmentStatus)
	}
	var storedPlan model.WorkPermitPlan
	if err := fixture.db.First(&storedPlan, plan.ID).Error; err != nil {
		t.Fatalf("reload plan: %v", err)
	}
	if storedPlan.PermitStatus != constants.PermitStatusAssessed {
		t.Fatalf("plan status = %q, want assessed (rolled back)", storedPlan.PermitStatus)
	}
	if storedPlan.Version != assessed.PlanVersion {
		t.Fatalf("plan version = %d, want %d (rolled back)", storedPlan.Version, assessed.PlanVersion)
	}

	// Restore the audit table and resubmit: it must now succeed and occupy once.
	if err := fixture.db.AutoMigrate(&model.AuditEvent{}); err != nil {
		t.Fatalf("recreate audit table: %v", err)
	}
	submitted, err := fixture.assessments.Submit(assessed.ID, dto.PlanVersionRequest{Version: assessed.PlanVersion}, fixture.planner, "req-rb-submit-ok")
	if err != nil {
		t.Fatalf("resubmit after audit recovery: %v", err)
	}
	if submitted.AssessmentStatus != constants.AssessmentStatusSubmitted {
		t.Fatalf("assessment status = %q, want submitted", submitted.AssessmentStatus)
	}
	occupation := fixture.occupationByAssessment(t, assessed.ID)
	if occupation.OccupationStatus != constants.OccupationStatusOccupied || occupation.DoseMSV != 0.315 {
		t.Fatalf("occupation = %+v, want occupied 0.315 mSv", occupation)
	}
	fixture.assertBalance(t, worker.ID, 0.315, 1, 0.315, constants.DoseBandWithinAdmin)
	if fixture.auditCount("budget.occupied") != 1 {
		t.Fatalf("budget.occupied events = %d, want exactly one after recovery", fixture.auditCount("budget.occupied"))
	}
}
