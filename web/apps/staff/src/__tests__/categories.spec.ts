import type { Category, CategoryAttribute } from '@pikni/api'
import { describe, expect, it } from 'vitest'

import { ancestors, schemaOf, treeOrder } from '../categories'
import { makeAttribute, makeCategory } from './fixtures'

const wine = makeCategory({ id: 'wine', name: 'Вино' })
const beer = makeCategory({ id: 'beer', name: 'Пиво и сидр' })
const sparkling = makeCategory({ id: 'sparkling', name: 'Игристое', parentId: 'wine', depth: 2 })
const brut = makeCategory({ id: 'brut', name: 'Брют', parentId: 'sparkling', depth: 3 })
const still = makeCategory({ id: 'still', name: 'Тихое', parentId: 'wine', depth: 2 })
const categories: Category[] = [wine, beer, sparkling, still, brut]

const attributes: CategoryAttribute[] = [
  makeAttribute({ id: 'color', categoryId: 'wine', code: 'color' }),
  makeAttribute({ id: 'method', categoryId: 'sparkling', code: 'method' }),
  makeAttribute({ id: 'dosage', categoryId: 'brut', code: 'dosage' }),
  makeAttribute({ id: 'style', categoryId: 'beer', code: 'style' }),
]

describe('categories', () => {
  it('treeOrder: подкатегории идут сразу за родителем', () => {
    expect(treeOrder(categories).map((c) => c.id)).toEqual([
      'wine',
      'sparkling',
      'brut',
      'still',
      'beer',
    ])
  })

  it('ancestors: от корня к родителю', () => {
    expect(ancestors(brut, categories).map((c) => c.id)).toEqual(['wine', 'sparkling'])
    expect(ancestors(wine, categories)).toEqual([])
  })

  it('schemaOf: сначала унаследованные с источником, затем свои', () => {
    const rows = schemaOf(brut, categories, attributes)
    expect(rows.map((r) => [r.attribute.code, r.from?.name])).toEqual([
      ['color', 'Вино'],
      ['method', 'Игристое'],
      ['dosage', undefined],
    ])
  })
})
