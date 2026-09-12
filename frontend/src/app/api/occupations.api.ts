import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { PageEnvelope } from '../types/api';
import { BudgetOccupation, OccupationStatus, WorkerBudgetBalance } from '../types/occupation';

@Injectable({ providedIn: 'root' })
export class OccupationsApi {
  private readonly http = inject(HttpClient);
  private readonly root = '/api/v1/budget-occupations';

  balances(profileStatus = 'active') {
    return this.http.get<PageEnvelope<WorkerBudgetBalance>>(`${this.root}/worker-balances`, {
      params: { page_size: 100, profile_status: profileStatus },
    });
  }

  list(status?: OccupationStatus | '', workerId?: number) {
    const params: Record<string, string | number> = { page_size: 100 };
    if (status) params['status'] = status;
    if (workerId) params['worker_id'] = workerId;
    return this.http.get<PageEnvelope<BudgetOccupation>>(this.root, { params });
  }
}
