/*
 * Repeatable page-state tests for the budget occupation ledger store.
 *
 * These run under Node's built-in test runner (see scripts/run-store-tests.mjs):
 * esbuild bundles the spec with the real Angular store/api classes, while the
 * HTTP layer is a controllable fake so responses can be resolved in any order.
 * No browser, zone.js or network is required, and each test gets a fresh
 * injector/store, so the suite is order-independent and re-runnable.
 */
import '@angular/compiler';
import { strict as assert } from 'node:assert';
import { test } from 'node:test';
import { HttpClient } from '@angular/common/http';
import { Injector, inject, runInInjectionContext } from '@angular/core';
import { Observable, Subscriber } from 'rxjs';
import { OccupationsApi } from '../api/occupations.api';
import { OccupationsStore } from './occupations.store';
import type { PageEnvelope } from '../types/api';
import type { BudgetOccupation, OccupationStatus, WorkerBudgetBalance } from '../types/occupation';

interface RecordedRequest {
  path: string;
  params: Record<string, string>;
  resolved: boolean;
  resolve: () => void;
}

class FakeHttp {
  readonly requests: RecordedRequest[] = [];
  private balancesBody: PageEnvelope<WorkerBudgetBalance> | null = defaultBalances();
  private balancesError: Error | null = null;

  failBalances(error: Error): void {
    this.balancesError = error;
  }

  get(url: string, options?: { params?: Record<string, string | number> }): Observable<unknown> {
    const path = url.replace(/^.*\/api\/v1/, '');
    const params: Record<string, string> = {};
    for (const [key, value] of Object.entries(options?.params ?? {})) {
      if (value !== '' && value !== undefined && value !== null) params[key] = String(value);
    }
    return new Observable((subscriber: Subscriber<unknown>) => {
      const request: RecordedRequest = {
        path,
        params,
        resolved: false,
        resolve: () => {
          request.resolved = true;
          if (path.endsWith('/worker-balances')) {
            if (this.balancesError) subscriber.error(this.balancesError);
            else {
              subscriber.next(this.balancesBody);
              subscriber.complete();
            }
            return;
          }
          subscriber.next(detailBodyFor(params));
          subscriber.complete();
        },
      };
      this.requests.push(request);
    });
  }

  resolveOne(predicate: (request: RecordedRequest) => boolean): void {
    const request = this.requests.find(candidate => !candidate.resolved && predicate(candidate));
    assert.ok(request, `no unresolved request matched; requests=${describe(this.requests)}`);
    request!.resolve();
  }
}

function describe(requests: RecordedRequest[]): string {
  return requests.map(request => `${request.path}?${new URLSearchParams(request.params).toString()}`).join(' | ');
}

function balance(id: number): WorkerBudgetBalance {
  return {
    worker_id: id, worker_code: `W${id}`, worker_name: `Worker ${id}`, authorization_level: 'L1',
    profile_status: 'active', period_start: '2026-01-01T00:00:00Z', administrative_limit_msv: 2,
    annual_legal_limit_msv: 20, verified_dose_msv: 0, active_occupation_msv: 0, committed_dose_msv: 0,
    remaining_admin_msv: 2, remaining_legal_msv: 20, active_occupation_count: 0, active_occupation_ids: [],
    risk_band: 'within_admin', requires_manual_review: false, escalation_explanation: '',
    threshold_version: 'ALARA-2026.1', planning_only: true, automatic_work_permit: false,
  };
}

function defaultBalances(): PageEnvelope<WorkerBudgetBalance> {
  return pageEnvelope([balance(1), balance(2)]);
}

function pageEnvelope<T>(data: T[]): PageEnvelope<T> {
  return { data, meta: { page: 1, page_size: 100, total: data.length, total_pages: 1 }, request_id: 'r' };
}

function occupation(id: number, worker: number, status: OccupationStatus): BudgetOccupation {
  return {
    id, worker_id: worker, worker_code: `W${worker}`, worker_name: `Worker ${worker}`, plan_id: worker * 10,
    plan_code: `P${worker}`, assessment_id: worker * 100 + id, occupation_status: status, dose_msv: 0.4,
    period_dose_msv: 0, occupied_by: 1, occupied_at: '2026-09-13T00:00:00Z',
    retained_by: status === 'retained' ? 2 : undefined,
    retained_at: status === 'retained' ? '2026-09-13T00:00:00Z' : undefined,
    released_by: status === 'released' ? 2 : undefined,
    released_at: status === 'released' ? '2026-09-13T00:00:00Z' : undefined,
    release_reason: status === 'released' ? 'review_rejected' : '', version: 1,
    planning_only: true, automatic_work_permit: false,
  };
}

// Deterministic payload per (worker, status) so stale responses are identifiable.
function detailBodyFor(params: Record<string, string>): PageEnvelope<BudgetOccupation> {
  const worker = params['worker_id'];
  const status = params['status'] as OccupationStatus | undefined;
  if (worker === '1') {
    if (status === 'released') return pageEnvelope([occupation(103, 1, 'released')]);
    if (status === 'retained') return pageEnvelope([]);
    return pageEnvelope([occupation(101, 1, 'occupied')]);
  }
  if (worker === '2') return pageEnvelope([occupation(202, 2, 'retained')]);
  // Unfiltered request — deliberately dominated by worker 2 to expose overwrite bugs.
  return pageEnvelope([occupation(202, 2, 'retained')]);
}

interface Harness {
  http: FakeHttp;
  store: OccupationsStore;
}

function newHarness(): Harness {
  const http = new FakeHttp();
  const injector = Injector.create({
    providers: [
      { provide: HttpClient, useValue: http as unknown as HttpClient },
      OccupationsApi,
      OccupationsStore,
    ],
  });
  const store = runInInjectionContext(injector, () => inject(OccupationsStore));
  return { http, store };
}

// 1) First load: balances arrive first and select worker 1; even if a stale
//    unfiltered detail request lands last, only worker 1 rows remain.
test('first load keeps only the selected worker detail', () => {
  const { http, store } = newHarness();
  store.loadBalances();

  http.resolveOne(request => request.path.endsWith('/worker-balances'));
  assert.equal(store.selectedWorkerId(), 1, 'first worker auto-selected');

  const isDetail = (request: RecordedRequest): boolean => !request.path.endsWith('/worker-balances');

  // The auto-loaded detail must be filtered to worker 1 (never unfiltered).
  const detail = http.requests.find(request => isDetail(request) && request.params['worker_id'] === '1');
  assert.ok(detail, 'expected a worker-1 filtered detail request');
  assert.equal(http.requests.find(request => isDetail(request) && !request.params['worker_id']), undefined,
    'first load must not fire an unfiltered detail request');

  // Correct worker-1 detail resolves; any stale unfiltered detail lands after.
  http.resolveOne(request => isDetail(request) && request.params['worker_id'] === '1');
  for (const stale of http.requests.filter(request => isDetail(request) && !request.resolved && !request.params['worker_id'])) {
    stale.resolve();
  }

  const rows = store.occupations();
  assert.ok(rows.length > 0, 'detail rows should be present');
  assert.ok(rows.every(row => row.worker_id === 1), `rows must all belong to worker 1, got ${rows.map(row => row.worker_id)}`);
});

// 2) Rapid switching 1 -> 2: the older worker-1 response resolves last and must
//    not overwrite the current worker-2 view.
test('rapid worker switching ignores the stale older response', () => {
  const { http, store } = newHarness();
  store.selectWorker(1);
  store.selectWorker(2);

  http.resolveOne(request => request.params['worker_id'] === '2'); // newer first
  http.resolveOne(request => request.params['worker_id'] === '1'); // stale older last

  assert.equal(store.selectedWorkerId(), 2);
  const rows = store.occupations();
  assert.ok(rows.length === 1 && rows[0].worker_id === 2,
    `rows must match current worker 2, got ${rows.map(row => row.worker_id)}`);
});

// 3) Status filter switching occupied -> released: the stale occupied response
//    lands last but the table must reflect the new released filter.
test('status filter switch keeps the newest filter result', () => {
  const { http, store } = newHarness();
  store.selectWorker(1);
  http.resolveOne(request => request.params['worker_id'] === '1');

  store.filterStatus('occupied');
  store.filterStatus('released');
  http.resolveOne(request => request.params['status'] === 'released'); // newest first
  http.resolveOne(request => request.params['status'] === 'occupied'); // stale last

  assert.equal(store.statusFilter(), 'released');
  const rows = store.occupations();
  assert.ok(rows.length === 1 && rows[0].occupation_status === 'released',
    `rows must match released filter, got ${rows.map(row => row.occupation_status)}`);
  assert.ok(rows.every(row => row.worker_id === 1), 'filtered rows still belong to worker 1');
});

// 4) A failed balances reload must not wipe valid detail already on screen.
test('balances reload failure keeps the existing valid detail', () => {
  const { http, store } = newHarness();

  // Establish a valid worker-1 view first.
  store.loadBalances();
  http.resolveOne(request => request.path.endsWith('/worker-balances'));
  http.resolveOne(request => request.params['worker_id'] === '1');
  assert.equal(store.selectedWorkerId(), 1);
  const before = store.occupations();
  assert.ok(before.length === 1 && before[0].worker_id === 1, 'valid worker-1 detail established');

  // A later balances refresh fails (e.g. transient network error).
  http.failBalances(new Error('network unavailable'));
  store.loadBalances();
  http.resolveOne(request => request.path.endsWith('/worker-balances'));

  // Selected worker and the existing detail remain intact.
  assert.equal(store.selectedWorkerId(), 1, 'selected worker survives balances failure');
  const after = store.occupations();
  assert.deepEqual(after, before, 'existing detail must not be cleared when balances fail');
});
