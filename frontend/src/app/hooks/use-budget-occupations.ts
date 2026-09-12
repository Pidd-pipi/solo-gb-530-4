import { inject } from '@angular/core';
import { OccupationsStore } from '../stores/occupations.store';

export function useBudgetOccupations(): OccupationsStore {
  return inject(OccupationsStore);
}
