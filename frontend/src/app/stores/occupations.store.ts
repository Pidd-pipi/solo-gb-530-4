import { Injectable, inject, signal } from '@angular/core';
import { OccupationsApi } from '../api/occupations.api';
import { BudgetOccupation, OccupationStatus, WorkerBudgetBalance } from '../types/occupation';

@Injectable({ providedIn: 'root' })
export class OccupationsStore {
  private readonly api = inject(OccupationsApi);
  readonly balances = signal<WorkerBudgetBalance[]>([]);
  readonly occupations = signal<BudgetOccupation[]>([]);
  readonly selectedWorkerId = signal<number | null>(null);
  readonly statusFilter = signal<OccupationStatus | ''>('');
  readonly loading = signal(false);

  loadBalances(): void {
    this.loading.set(true);
    this.api.balances().subscribe({
      next: response => {
        this.balances.set(response.data);
        const selected = this.selectedWorkerId();
        if ((selected === null || !response.data.some(item => item.worker_id === selected)) && response.data.length) {
          this.selectedWorkerId.set(response.data[0].worker_id);
          this.loadOccupations();
        }
        this.loading.set(false);
      },
      error: () => this.loading.set(false),
    });
  }

  loadOccupations(): void {
    const workerId = this.selectedWorkerId() ?? undefined;
    this.api.list(this.statusFilter(), workerId).subscribe({
      next: response => this.occupations.set(response.data),
      error: () => this.occupations.set([]),
    });
  }

  selectWorker(workerId: number): void {
    this.selectedWorkerId.set(workerId);
    this.loadOccupations();
  }

  filterStatus(status: OccupationStatus | ''): void {
    this.statusFilter.set(status);
    this.loadOccupations();
  }

  get overLimitCount(): number {
    return this.balances().filter(item => item.requires_manual_review).length;
  }
}
