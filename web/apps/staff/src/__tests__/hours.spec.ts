import { describe, expect, it } from 'vitest'

import { formatHours } from '../hours'

describe('formatHours', () => {
  const day = (open: string, close: string) => ({ open, close })

  it.each([
    [{}, 'пн–вс выходной'],
    [
      {
        mon: day('09:00', '23:00'),
        tue: day('09:00', '23:00'),
        wed: day('09:00', '23:00'),
        thu: day('09:00', '23:00'),
        fri: day('09:00', '23:00'),
        sat: day('10:00', '23:00'),
      },
      'пн–пт 09:00–23:00, сб 10:00–23:00, вс выходной',
    ],
    [
      Object.fromEntries(
        ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'].map((d) => [d, day('10:00', '22:00')]),
      ),
      'ежедневно 10:00–22:00',
    ],
  ])('%j → %s', (hours, text) => {
    expect(formatHours(hours)).toBe(text)
  })
})
