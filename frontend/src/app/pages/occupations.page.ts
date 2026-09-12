import { ChangeDetectionStrategy, Component, OnInit, computed } from '@angular/core';
import { CommonModule } from '@angular/common';
import { MatButtonModule } from '@angular/material/button';
import { SafetyBoundaryBannerComponent } from '../components/common/safety-boundary-banner.component';
import { useBudgetOccupations } from '../hooks/use-budget-occupations';
import { BudgetOccupation, OccupationStatus, WorkerBudgetBalance } from '../types/occupation';
import { DoseBand } from '../types/dose';

@Component({
  standalone: true,
  imports: [CommonModule, MatButtonModule, SafetyBoundaryBannerComponent],
  template: `
    <div class="page">
      <header class="page-head">
        <div>
          <span class="eyebrow">预算占用与释放台</span>
          <h1>人员剂量预算占用台账</h1>
          <p>规划员提交评估即按计划新增剂量占用预算；RPO 接受后保留，拒绝或归档后释放。同一评估仅占用一次。</p>
        </div>
      </header>
      <app-safety-boundary-banner
        title="仅用于离线 ALARA 规划"
        detail="本台账只反映规划预算的占用与释放，不是剂量计、医疗结论或作业许可控制器；任何状态均不构成允许作业的结论。" />

      <p class="notice-banner" *ngIf="store.overLimitCount > 0">
        ⚠ 有 {{ store.overLimitCount }} 名人员的已承诺剂量触及或超过配置阈值，仅提示 RPO 人工复核，系统不会生成任何作业许可。
      </p>

      <div class="metric-strip">
        <div class="metric"><strong>{{ store.balances().length }}</strong><span>在岗人员</span></div>
        <div class="metric"><strong>{{ store.overLimitCount }}</strong><span>需人工复核</span></div>
        <div class="metric"><strong>{{ totalActiveMSv() | number:'1.3-3' }}</strong><span>有效占用合计 mSv</span></div>
        <div class="metric"><strong>{{ totalOccupations() }}</strong><span>有效占用条目</span></div>
      </div>

      <div class="section-title">
        <h2>人员预算余额</h2>
        <span>已核验剂量、有效占用与剩余额度合并为单一视图</span>
      </div>
      <div class="split">
        <div class="surface">
          <table class="data-table balances">
            <thead><tr><th>人员</th><th>已核验</th><th>有效占用</th><th>已承诺</th><th>剩余(行政)</th><th>剩余(法规)</th><th>风险</th></tr></thead>
            <tbody>
              <tr *ngFor="let item of store.balances()"
                  [class.selected]="store.selectedWorkerId() === item.worker_id"
                  (click)="store.selectWorker(item.worker_id)">
                <td><strong>{{ item.worker_name }}</strong><br><small class="muted code">{{ item.worker_code }}</small></td>
                <td>{{ item.verified_dose_msv | number:'1.3-3' }}</td>
                <td>{{ item.active_occupation_msv | number:'1.3-3' }}<br><small class="muted">{{ item.active_occupation_count }} 条</small></td>
                <td><strong>{{ item.committed_dose_msv | number:'1.3-3' }}</strong></td>
                <td [class.over]="item.remaining_admin_msv === 0">{{ item.remaining_admin_msv | number:'1.3-3' }}</td>
                <td [class.over]="item.remaining_legal_msv === 0">{{ item.remaining_legal_msv | number:'1.3-3' }}</td>
                <td>
                  <span class="band" [ngClass]="item.risk_band">{{ bandLabel(item.risk_band) }}</span>
                  <small class="review" *ngIf="item.requires_manual_review">需人工复核</small>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="empty" *ngIf="!store.balances().length">暂无在岗人员预算数据。</div>
        </div>

        <div class="detail">
          <ng-container *ngIf="selected() as worker">
            <section class="surface worker-card">
              <header>
                <div><strong>{{ worker.worker_name }}</strong><span class="code">{{ worker.worker_code }} · {{ worker.authorization_level }}</span></div>
                <span class="band" [ngClass]="worker.risk_band">{{ bandLabel(worker.risk_band) }}</span>
              </header>
              <p class="formula">
                已承诺剂量 = 已核验 {{ worker.verified_dose_msv | number:'1.3-3' }}
                + 有效占用 {{ worker.active_occupation_msv | number:'1.3-3' }}
                = <strong>{{ worker.committed_dose_msv | number:'1.3-3' }} mSv</strong>
              </p>
              <dl class="quota">
                <div><dt>行政控制值</dt><dd>{{ worker.administrative_limit_msv | number:'1.3-3' }}</dd><dt>剩余行政余量</dt><dd [class.over]="worker.remaining_admin_msv === 0">{{ worker.remaining_admin_msv | number:'1.3-3' }}</dd></div>
                <div><dt>法规规划限值</dt><dd>{{ worker.annual_legal_limit_msv | number:'1.3-3' }}</dd><dt>剩余法规余量</dt><dd [class.over]="worker.remaining_legal_msv === 0">{{ worker.remaining_legal_msv | number:'1.3-3' }}</dd></div>
              </dl>
              <p class="notice-banner" *ngIf="worker.requires_manual_review">{{ worker.escalation_explanation }}（阈值版本 {{ worker.threshold_version }}）— 仅提示人工复核，不产生作业许可。</p>
            </section>

            <div class="section-title"><h2>占用变化记录</h2><span>提交 / 接受 / 拒绝 / 归档均可在此回看</span></div>
            <div class="filters">
              <button mat-button *ngFor="let option of statusOptions"
                      [class.active]="store.statusFilter() === option.value"
                      (click)="store.filterStatus(option.value)">{{ option.label }}</button>
            </div>
            <div class="surface">
              <table class="data-table ledger">
                <thead><tr><th>占用时间(UTC)</th><th>计划</th><th>评估 #</th><th>占用剂量</th><th>状态</th><th>释放时间 / 原因</th></tr></thead>
                <tbody>
                  <tr *ngFor="let row of store.occupations()">
                    <td class="code">{{ row.occupied_at | date:'yyyy-MM-dd HH:mm':'UTC' }}</td>
                    <td><strong>{{ row.plan_code }}</strong></td>
                    <td class="code">#{{ row.assessment_id }}</td>
                    <td>{{ row.dose_msv | number:'1.3-3' }} mSv<br><small class="muted">提交时期间 {{ row.period_dose_msv | number:'1.3-3' }}</small></td>
                    <td><span class="state" [ngClass]="row.occupation_status">{{ statusLabel(row.occupation_status) }}</span></td>
                    <td>
                      <ng-container *ngIf="row.released_at; else retainedTpl">
                        <small class="code">{{ row.released_at | date:'yyyy-MM-dd HH:mm':'UTC' }}</small><br>
                        <small class="muted">{{ reasonLabel(row.release_reason) }}</small>
                      </ng-container>
                      <ng-template #retainedTpl>
                        <small class="muted" *ngIf="row.retained_at; else activeTpl">RPO 接受保留</small>
                        <ng-template #activeTpl><small class="muted">等待 RPO 复核</small></ng-template>
                      </ng-template>
                    </td>
                  </tr>
                </tbody>
              </table>
              <div class="empty" *ngIf="!store.occupations().length">该人员当前筛选条件下没有占用记录。</div>
            </div>
            <p class="hint">台账为只读：占用随评估提交创建，RPO 接受后保留，拒绝或计划归档后释放；所有变化均写入审计。</p>
          </ng-container>
          <div class="empty surface" *ngIf="!selected()">选择左侧人员查看预算余额与占用历史。</div>
        </div>
      </div>
    </div>
  `,
  styles: [`
    .balances tr { cursor: pointer; }
    .balances td small { display: block; margin-top: 2px; }
    .over { color: #8c2929; font-weight: 800; }
    .review { display: inline-block; margin-top: 6px; padding: 2px 7px; border: 1px solid #d6b262; border-radius: 3px; color: #76510b; background: #fff1ca; font-size: 10px; font-weight: 700; }
    .band { display: inline-flex; align-items: center; padding: 3px 9px; border: 1px solid; border-radius: 3px; font-size: 11px; font-weight: 800; white-space: nowrap; }
    .within_admin { color: #185847; border-color: #8eb8a8; background: #e5f1eb; }
    .above_admin { color: #76510b; border-color: #d6b262; background: #fff1ca; }
    .near_legal { color: #823f11; border-color: #dd9b67; background: #fbe5d4; }
    .above_legal, .invalid { color: #8c2929; border-color: #d89591; background: #f8dfde; }
    .worker-card { padding: 16px 18px; margin-bottom: 6px; }
    .worker-card header { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 10px; }
    .worker-card header span:first-child { display: flex; flex-direction: column; gap: 3px; }
    .worker-card .code { color: var(--muted); font-size: 11px; }
    .formula { margin: 0 0 12px; font-size: 13px; }
    .quota { margin: 0; display: grid; gap: 8px; }
    .quota > div { display: grid; grid-template-columns: 1fr auto 1fr auto; gap: 14px; font-size: 12px; }
    dt { color: var(--muted); } dd { margin: 0; font-variant-numeric: tabular-nums; font-weight: 700; }
    .filters { display: flex; gap: 6px; margin-bottom: 10px; }
    .filters button { font-size: 12px; }
    .filters button.active { background: #e8eeea; font-weight: 800; }
    .ledger td small { display: inline-block; }
    .state { display: inline-flex; padding: 3px 9px; border: 1px solid; border-radius: 3px; font-size: 11px; font-weight: 800; white-space: nowrap; }
    .state.occupied { color: #76510b; border-color: #d6b262; background: #fff1ca; }
    .state.retained { color: #185847; border-color: #8eb8a8; background: #e5f1eb; }
    .state.released { color: var(--muted); border-color: var(--line); background: #eef0ec; }
    .hint { margin-top: 10px; color: var(--muted); font-size: 11px; }
    @media (max-width: 980px) { .data-table { min-width: 720px; } .surface { overflow-x: auto; } }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class OccupationsPage implements OnInit {
  readonly store = useBudgetOccupations();
  readonly statusOptions: { value: OccupationStatus | ''; label: string }[] = [
    { value: '', label: '全部' },
    { value: 'occupied', label: '占用中' },
    { value: 'retained', label: '已保留' },
    { value: 'released', label: '已释放' },
  ];

  ngOnInit(): void {
    this.store.loadBalances();
    this.store.loadOccupations();
  }

  readonly selected = computed<WorkerBudgetBalance | null>(() => {
    const id = this.store.selectedWorkerId();
    return this.store.balances().find(item => item.worker_id === id) ?? null;
  });
  readonly totalActiveMSv = computed(() =>
    this.store.balances().reduce((sum, item) => sum + item.active_occupation_msv, 0));
  readonly totalOccupations = computed(() =>
    this.store.balances().reduce((sum, item) => sum + item.active_occupation_count, 0));

  bandLabel(band: DoseBand): string {
    return {
      within_admin: '行政值内',
      above_admin: '超行政值',
      near_legal: '接近法规值',
      above_legal: '超法规值',
      invalid: '输入无效',
    }[band];
  }

  statusLabel(status: BudgetOccupation['occupation_status']): string {
    return { occupied: '占用中', retained: '已保留', released: '已释放' }[status];
  }

  reasonLabel(reason: string): string {
    return { review_rejected: 'RPO 拒绝后释放', plan_archived: '计划归档后释放', '': '' }[reason] ?? reason;
  }
}
