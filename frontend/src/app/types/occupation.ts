import { DoseBand } from './dose';

export type OccupationStatus = 'occupied' | 'retained' | 'released';
export type OccupationReleaseReason = 'review_rejected' | 'plan_archived' | '';

export interface BudgetOccupation {
  id: number;
  worker_id: number;
  worker_code: string;
  worker_name: string;
  plan_id: number;
  plan_code: string;
  assessment_id: number;
  occupation_status: OccupationStatus;
  dose_msv: number;
  period_dose_msv: number;
  occupied_by: number;
  occupied_at: string;
  retained_by?: number;
  retained_at?: string;
  released_by?: number;
  released_at?: string;
  release_reason: OccupationReleaseReason;
  version: number;
  planning_only: boolean;
  automatic_work_permit: boolean;
}

export interface WorkerBudgetBalance {
  worker_id: number;
  worker_code: string;
  worker_name: string;
  authorization_level: string;
  profile_status: string;
  period_start: string;
  administrative_limit_msv: number;
  annual_legal_limit_msv: number;
  verified_dose_msv: number;
  active_occupation_msv: number;
  committed_dose_msv: number;
  remaining_admin_msv: number;
  remaining_legal_msv: number;
  active_occupation_count: number;
  active_occupation_ids: number[];
  risk_band: DoseBand;
  requires_manual_review: boolean;
  escalation_explanation: string;
  threshold_version: string;
  planning_only: boolean;
  automatic_work_permit: boolean;
}
