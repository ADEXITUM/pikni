import { describe, expect, it } from 'vitest'

import { attributeValues, fitAttributes } from '../products'
import { makeAttribute } from './fixtures'

const raw = makeAttribute({ id: 'raw', categoryId: 'vodka', code: 'raw_material', name: 'Сырьё' })
const aging = makeAttribute({
  id: 'aging',
  categoryId: 'whisky',
  code: 'aging',
  name: 'Выдержка',
  type: 'number',
})

describe('products', () => {
  it('fitAttributes оставляет характеристики новой схемы и называет убранные', () => {
    expect(fitAttributes({ raw_material: 'рожь', aging: 3, empty: '' }, [raw], [aging])).toEqual({
      values: { aging: 3 },
      dropped: ['Сырьё'],
    })
  })

  it('attributeValues убирает пустые и переводит числа из поля ввода', () => {
    expect(attributeValues({ raw_material: 'рожь', aging: '5,5' }, [raw, aging])).toEqual({
      raw_material: 'рожь',
      aging: 5.5,
    })
    expect(attributeValues({ raw_material: null, aging: '' }, [raw, aging])).toEqual({})
  })
})
