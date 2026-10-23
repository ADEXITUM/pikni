import type { AuditEntry } from '@pikni/api'
import { describe, expect, it } from 'vitest'

import { auditActor, auditDetails, auditObject } from '../audit'

const codes = new Map([
  ['mg-1', 'МГ-01'],
  ['mg-2', 'МГ-02'],
])

function entry(e: Partial<AuditEntry>): AuditEntry {
  return { id: 1, at: '2026-10-23T10:30:00Z', action: 'auth.login', data: {}, ...e }
}

describe('auditDetails', () => {
  it('неудачный вход: причина и блокировка', () => {
    const e = entry({
      action: 'auth.login_failed',
      ip: '203.0.113.7',
      entityType: 'user',
      data: { reason: 'password', locked: true, login: 'petrova' },
    })
    expect(auditDetails(e, codes)).toEqual([
      { label: 'Причина', value: 'неверный пароль' },
      { label: 'Итог', value: 'вход закрыт на 15 минут' },
      { label: 'IP', value: '203.0.113.7' },
    ])
    expect(auditActor(e)).toBe('—')
    expect(auditObject(e)).toBe('petrova')
  })

  it('изменение сотрудника: было → стало', () => {
    const e = entry({
      action: 'user.updated',
      data: {
        login: 'kassir',
        changes: {
          fullName: ['Кассир', 'Кассир Иванова'],
          role: ['cashier', 'storekeeper'],
          locationIds: [['mg-1'], ['mg-1', 'mg-2']],
        },
      },
    })
    expect(auditDetails(e, codes)).toEqual([
      { label: 'ФИО', value: 'Кассир → Кассир Иванова' },
      { label: 'Роль', value: 'Продавец-кассир → Кладовщик' },
      { label: 'Точки', value: 'МГ-01 → МГ-01, МГ-02' },
    ])
  })

  it('изменение точки: поля по-русски, да/нет, режим работы кратко', () => {
    const e = entry({
      action: 'location.updated',
      entityType: 'location',
      data: {
        code: 'МГ-01',
        changes: {
          address: [null, 'ул. Ленина, 114'],
          isActive: [true, false],
          openingHours: [{}, { mon: { open: '10:00', close: '22:00' } }],
        },
      },
    })
    expect(auditDetails(e, codes)).toEqual(
      expect.arrayContaining([
        { label: 'Адрес', value: '— → ул. Ленина, 114' },
        { label: 'Действует', value: 'да → нет' },
        { label: 'Режим работы', value: 'пн–вс выходной → пн 10:00–22:00, вт–вс выходной' },
      ]),
    )
    expect(auditObject(e)).toBe('МГ-01')
  })

  it('незнакомые поля показывает как есть', () => {
    const e = entry({ action: 'auth.logout', data: { device: 'TC21' } })
    expect(auditDetails(e, codes)).toEqual([{ label: 'device', value: '"TC21"' }])
    expect(auditActor(e)).toBe('Система')
  })
})
