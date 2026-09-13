import { Injectable, inject, signal } from '@angular/core';
import { OccupationsApi } from '../api/occupations.api';
import { BudgetOccupation, OccupationStatus, WorkerBudgetBalance } from '../types/occupation';

interface DetailKey {
  workerId: number;
  status: OccupationStatus | '';
}

@Injectable({ providedIn: 'root' })
export class OccupationsStore {
  private readonly api = inject(OccupationsApi);
  readonly balances = signal<WorkerBudgetBalance[]>([]);
  readonly occupations = signal<BudgetOccupation[]>([]);
  readonly selectedWorkerId = signal<number | null>(null);
  readonly statusFilter = signal<OccupationStatus | ''>('');
  readonly loading = signal(false);

  // Monotonic generation for detail requests. A response from an older
  // generation (a stale worker switch / filter change, or a late first-load
  // request) is discarded, so the table can never be overwritten by a result
  // that no longer matches the selected worker.
  private detailGeneration = 0;

  loadBalances(): void {
    this.loading.set(true);
    this.api.balances().subscribe({
      next: response => {
        this.balances.set(response.data);
        const selected = this.selectedWorkerId();
        if (selected !== null && response.data.some(item => item.worker_id === selected)) {
          this.refreshDetail();
        } else if (response.data.length) {
          // First load: pick the first worker and load their detail explicitly.
          this.selectWorker(response.data[0].worker_id);
        } else {
          this.occupations.set([]);
        }
        this.loading.set(false);
      },
      error: () => this.loading.set(false),
    });
  }

  selectWorker(workerId: number): void {
    this.selectedWorkerId.set(workerId);
    this.loadDetail({ workerId, status: this.statusFilter() });
  }

  filterStatus(status: OccupationStatus | ''): void {
    this.statusFilter.set(status);
    this.refreshDetail();
  }

  /** Reload the ledger for the currently selected worker and status filter. */
  refreshDetail(): void {
    const workerId = this.selectedWorkerId();
    if (workerId === null) {
      this.occupations.set([]);
      return;
    }
    this.loadDetail({ workerId, status: this.statusFilter() });
  }

  private loadDetail(key: DetailKey): void {
    const generation = ++this.detailGeneration;
    this.api.list(key.status, key.workerId).subscribe({
      next: response => {
        // Drop stale responses (older worker/filter selection).
        if (generation !== this.detailGeneration) return;
        // Defense in depth: ignore a payload whose rows do not all match the
        // key that was requested, so an unfiltered result can never win.
        if (!response.data.every(row =>
          row.worker_id === key.workerId && (key.status === '' || row.occupation_status === key.status))) {
          return;
        }
        this.occupations.set(response.data);
      },
      error: () => {
        if (generation !== this.detailGeneration) return;
        this.occupations.set([]);
      },
    });
  }

  get overLimitCount(): number {
    return this.balances().filter(item => item.requires_manual_review).length;
  }
}
