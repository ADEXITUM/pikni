import { api, type Permission, type SaleBan, type SaleRule } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { makeMe } from '@/__tests__/fixtures'
import { meQuery } from '@/session'

import SaleRulesPage from '../SaleRulesPage.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: { GET: vi.fn<ApiModule['api']['GET']>() },
}))

const rule = (
  id: string,
  subjectCode: string | undefined,
  validFrom: string,
  from: string,
  to: string,
): SaleRule => ({
  id,
  subjectCode,
  scope: 'all',
  validFrom,
  from,
  to,
  source: `закон ${id}`,
})
const rules = [
  rule('fed', undefined, '2012-01-01', '08:00', '23:00'),
  rule('amur', '28', '2016-01-01', '11:00', '21:00'),
  rule('amur-next', '28', '2999-01-01', '12:00', '20:00'),
]
const bans: SaleBan[] = [
  {
    id: 'b1',
    subjectCode: '25',
    name: 'День знаний',
    scope: 'all',
    monthDay: '09-01',
    source: 'закон 536-КЗ',
  },
]

async function mountPage(permissions: Permission[]) {
  const queryClient = new QueryClient()
  queryClient.setQueryData(meQuery.queryKey, makeMe({ permissions }))
  const wrapper = mount(SaleRulesPage, {
    global: { plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient }]] },
  })
  await flushPromises()
  return wrapper
}

describe('SaleRulesPage', () => {
  beforeEach(() => {
    vi.mocked(api.GET).mockImplementation(((path: string) =>
      Promise.resolve({
        data:
          path === '/sale-rules'
            ? { rules, bans }
            : {
                items: [
                  { code: '25', name: 'Приморский край' },
                  { code: '28', name: 'Амурская область' },
                ],
              },
        response: new Response(),
      })) as never)
  })

  it('показывает правила по регионам с отметкой о действии', async () => {
    const wrapper = await mountPage(['refs.view'])
    const cards = wrapper.findAll('.v-card').map((c) => c.text())

    expect(cards[0]).toContain('Вся Россия')
    expect(cards[0]).toContain('08:00–23:00')
    const amur = cards.find((t) => t.includes('Амурская область'))!
    expect(amur).toContain('11:00–21:00')
    expect(amur).toContain('действует')
    expect(amur).toContain('вступит в силу')
    expect(cards.find((t) => t.includes('Приморский край'))).toContain(
      'День знаний — ежегодно 1 сентября',
    )
    expect(wrapper.text()).not.toContain('Новая редакция')
  })

  it('удалить можно только будущую редакцию', async () => {
    const wrapper = await mountPage(['refs.view', 'refs.edit'])

    expect(wrapper.find('[data-test="rule-amur-next"] button').exists()).toBe(true)
    expect(wrapper.find('[data-test="rule-amur"] button').exists()).toBe(false)
  })
})
