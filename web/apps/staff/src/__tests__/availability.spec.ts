import { describe, expect, it } from 'vitest'

import { filtersFromQuery, queryFromFilters } from '../availability'

describe('availability filters', () => {
  it('переживают адрес страницы и не пишут в него пустое', () => {
    const f = filtersFromQuery({
      q: 'тайга',
      volumeMl: '500',
      inStock: '1',
      sort: 'price',
      attrs: JSON.stringify({ color: 'красное', age: { min: 3 }, empty: {} }),
    })
    expect(f).toMatchObject({ q: 'тайга', volumeMl: 500, inStock: true, sort: 'price' })
    expect(queryFromFilters(f)).toEqual({
      q: 'тайга',
      volumeMl: '500',
      inStock: '1',
      sort: 'price',
      attrs: JSON.stringify({ color: 'красное', age: { min: 3 } }),
    })
    expect(queryFromFilters(filtersFromQuery({ sort: 'bogus', attrs: '{oops' }))).toEqual({})
  })
})
