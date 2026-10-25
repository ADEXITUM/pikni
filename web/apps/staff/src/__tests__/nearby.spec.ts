import { describe, expect, it } from 'vitest'

import { distanceKm, formatDistance, inScope } from '../nearby'

const msk = { city: 'Москва', subjectCode: '77', lat: 55.756, lon: 37.617 }
const spb = { city: 'Санкт-Петербург', subjectCode: '78', lat: 59.939, lon: 30.316 }
const balashikha = { city: 'Балашиха', subjectCode: '50' }
const podolsk = { city: 'Подольск', subjectCode: '50' }

describe('nearby', () => {
  it('сужает поиск до города и региона своей точки', () => {
    expect(inScope('city', msk, { ...msk })).toBe(true)
    expect(inScope('city', msk, spb)).toBe(false)
    expect(inScope('region', balashikha, podolsk)).toBe(true)
    expect(inScope('city', balashikha, podolsk)).toBe(false)
    expect(inScope('network', msk, spb)).toBe(true)
    expect(inScope('city', undefined, spb)).toBe(true)
  })

  it('считает расстояние по прямой', () => {
    expect(distanceKm(msk, spb)).toBeGreaterThan(620)
    expect(distanceKm(msk, spb)).toBeLessThan(650)
    expect(distanceKm(msk, balashikha)).toBeUndefined()
    expect(formatDistance(0.35)).toBe('350 м')
    expect(formatDistance(2.46)).toBe('2,5 км')
    expect(formatDistance(634.2)).toBe('634 км')
  })
})
