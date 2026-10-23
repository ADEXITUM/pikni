import { api, type Location, type Me, type User } from '@pikni/api'
import { createPikniVuetify } from '@pikni/ui'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { VSelect } from 'vuetify/components'

import { makeLocation } from '@/__tests__/fixtures'
import { meQuery } from '@/session'

import UserFormDialog from '../UserFormDialog.vue'

type ApiModule = typeof import('@pikni/api')

vi.mock('@pikni/api', async (importOriginal) => ({
  ...(await importOriginal<ApiModule>()),
  api: {
    GET: vi.fn<ApiModule['api']['GET']>(),
    POST: vi.fn<ApiModule['api']['POST']>(),
    PUT: vi.fn<ApiModule['api']['PUT']>(),
  },
}))

const locations: Location[] = [
  makeLocation({
    id: 'mg-1',
    code: 'МГ-01',
    name: 'Магазин',
    kind: 'store',
    city: 'Благовещенск',
    timezone: 'Asia/Yakutsk',
  }),
]

const me: Me = {
  id: 'admin',
  login: 'admin',
  fullName: 'Администратор',
  role: 'admin',
  mustChangePassword: false,
  csrfToken: 'csrf',
  permissions: ['users.manage'],
  locationIds: [],
}

const cashier: User = {
  id: 'u-1',
  login: 'kassir',
  fullName: 'Кассир',
  role: 'cashier',
  locationIds: ['mg-1'],
  isBlocked: false,
  mustChangePassword: false,
  version: 3,
  createdAt: '2026-10-01T00:00:00Z',
}

// Окно Vuetify рендерится в отдельный контейнер; для проверки формы достаточно содержимого.
const DialogStub = defineComponent({
  props: { modelValue: Boolean },
  setup:
    (props, { slots }) =>
    () =>
      props.modelValue ? slots.default?.() : null,
})

async function mountDialog(user: User | null) {
  const queryClient = new QueryClient()
  queryClient.setQueryData(meQuery.queryKey, me)
  const wrapper = mount(UserFormDialog, {
    props: { user, modelValue: false },
    global: {
      plugins: [createPikniVuetify(), [VueQueryPlugin, { queryClient }]],
      stubs: { VDialog: DialogStub },
    },
  })
  await wrapper.setProps({ modelValue: true })
  await flushPromises()
  return wrapper
}

describe('UserFormDialog', () => {
  beforeEach(() => {
    vi.mocked(api.GET).mockResolvedValue({
      data: { items: locations },
      response: new Response(),
    } as never)
    vi.mocked(api.POST).mockReset()
    vi.mocked(api.PUT).mockReset()
  })

  it('при изменении отправляет версию открытой карточки', async () => {
    vi.mocked(api.PUT).mockResolvedValue({
      data: { ...cashier, version: 4 },
      response: new Response(),
    } as never)
    const wrapper = await mountDialog(cashier)

    expect(wrapper.find('[data-test="locations"]').exists()).toBe(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.PUT).toHaveBeenCalledWith('/users/{id}', {
      params: { path: { id: 'u-1' } },
      body: { fullName: 'Кассир', role: 'cashier', locationIds: ['mg-1'], version: 3 },
    })
    expect(wrapper.emitted('saved')?.[0]).toEqual([{ ...cashier, version: 4 }])
  })

  it('показывает конфликт версий, не закрываясь', async () => {
    vi.mocked(api.PUT).mockResolvedValue({
      error: {
        type: 'about:blank',
        title: 'Конфликт',
        status: 409,
        detail: 'Данные сотрудника уже изменил другой администратор',
      },
      response: new Response(null, { status: 409 }),
    } as never)
    const wrapper = await mountDialog(cashier)

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Данные сотрудника уже изменил другой администратор')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('для роли уровня сети прячет точки и не отправляет их', async () => {
    vi.mocked(api.POST).mockResolvedValue({
      data: {
        user: { ...cashier, role: 'manager', locationIds: [] },
        temporaryPassword: 'abcdefgh2345',
      },
      response: new Response(),
    } as never)
    const wrapper = await mountDialog(null)

    await wrapper.find('[data-test="login"] input').setValue('boss')
    await wrapper.find('[data-test="full-name"] input').setValue('Руководитель')
    await wrapper.findComponent(VSelect).setValue('manager')
    expect(wrapper.find('[data-test="locations"]').exists()).toBe(false)

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(api.POST).toHaveBeenCalledWith('/users', {
      body: { login: 'boss', fullName: 'Руководитель', role: 'manager', locationIds: [] },
    })
    expect(wrapper.emitted('created')?.[0]?.[1]).toBe('abcdefgh2345')
  })
})
