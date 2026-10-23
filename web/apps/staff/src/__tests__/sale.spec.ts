import type { SaleRule } from '@pikni/api'
import { describe, expect, it } from 'vitest'

import { banDate, describeVerdict, editionStatuses } from '../sale'

describe('describeVerdict', () => {
  it.each([
    [{ allowed: true, from: '11:00', to: '21:00' }, 'можно до 21:00'],
    [{ allowed: false, from: '11:00', to: '21:00' }, 'нельзя, продажа с 11:00 до 21:00'],
    [{ allowed: false, banDay: 'День знаний' }, 'нельзя весь день: День знаний'],
  ])('%j → %s', (verdict, text) => {
    expect(describeVerdict(verdict)).toBe(text)
  })
})

describe('banDate', () => {
  it('ежегодный и разовый', () => {
    const base = { id: '1', subjectCode: '25', name: 'x', scope: 'all' as const, source: 'x' }
    expect(banDate({ ...base, monthDay: '09-01' })).toBe('ежегодно 1 сентября')
    expect(banDate({ ...base, date: '2027-05-25' })).toBe('25.05.2027')
  })
})

describe('editionStatuses', () => {
  it('действующая — последняя наступившая, остальные прежние или будущие', () => {
    const rule = (id: string, validFrom: string, scope: SaleRule['scope'] = 'all'): SaleRule => ({
      id,
      subjectCode: '28',
      scope,
      validFrom,
      from: '10:00',
      to: '22:00',
      source: 'x',
    })
    const statuses = editionStatuses(
      [
        rule('old', '2016-01-01'),
        rule('now', '2024-03-01'),
        rule('next', '2027-01-01'),
        rule('s', '2020-01-01', 'strong'),
      ],
      '2026-10-23',
    )
    expect(Object.fromEntries(statuses)).toEqual({
      old: 'past',
      now: 'current',
      next: 'planned',
      s: 'current',
    })
  })
})
