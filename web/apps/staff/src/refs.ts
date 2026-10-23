import {
  api,
  ApiError,
  type Brand,
  type Country,
  type EgaisWriteoffType,
  type OriginRegion,
  type Problem,
  type Producer,
  type ProductKind,
  type Volume,
  type WriteoffReason,
} from '@pikni/api'

export type RefValues = Record<string, unknown>

export interface RefField {
  key: string
  label: string
  type?: 'text' | 'select'
  items?: () => { value: string; title: string }[]
  optional?: boolean
  createOnly?: boolean
  hint?: string
}

// Колбэки объявлены методами: так конфиг конкретного справочника, например RefConfig<Producer>,
// подходит под общий RefConfig<object> и все вкладки рисует один компонент.
export interface RefConfig<Row> {
  key: string
  title: string
  rowKey(row: Row): string | number
  load(): Promise<Row[]>
  columns: { key: string; title: string; value?(row: Row): string }[]
  // Без полей справочник только для чтения.
  fields?: RefField[]
  hasActive?: boolean
  toForm?(row: Row): RefValues
  create?(values: RefValues): Promise<unknown>
  update?(row: Row, values: RefValues): Promise<unknown>
}

async function call<T>(request: Promise<{ data?: T; error?: Problem }>): Promise<T> {
  const { data, error } = await request
  if (error) throw new ApiError(error)
  return data as T
}

const text = (v: unknown) => String(v ?? '').trim()
const optional = (v: unknown) => text(v) || undefined

export function formatVolume(ml: number): string {
  return `${(ml / 1000).toLocaleString('ru')} л`
}

// Типы списания ЕГАИС с пояснениями из схемы EGCommonEnum.xsd. Record требует все значения
// перечисления: новый тип в контракте не скомпилируется без пояснения.
export const egaisWriteoffTypes: Record<EgaisWriteoffType, string> = {
  Пересортица: 'недостача по пересортице',
  Недостача: 'хищение, ошибки учёта, естественная убыль',
  Уценка: 'уценка или списание в результате порчи',
  Порча: 'бой, истёк срок годности, порча',
  Потери: 'потери при транспортировке',
  Проверки: 'лабораторные нужды, рекламные образцы',
  Арест: 'арестованная, изъятая, конфискованная продукция',
  'Иные цели': 'когда марку невозможно считать',
  Реализация: 'розничная продажа, которая не фиксируется в ЕГАИС',
  'Производственные потери': 'потери на производстве',
  Приготовление: 'использовано для коктейлей или блюд',
}

// buildRefs описывает справочники. Списки стран и производителей нужны для выбора
// в формах и для подписей в таблицах, поэтому передаются функциями.
export function buildRefs(lookup: {
  countries: () => Country[]
  producers: () => Producer[]
}): RefConfig<object>[] {
  const countryName = (code: string) =>
    lookup.countries().find((c) => c.code === code)?.name ?? code
  const producerName = (id: string) => lookup.producers().find((p) => p.id === id)?.name ?? '…'
  const countryItems = () => lookup.countries().map((c) => ({ value: c.code, title: c.name }))
  const producerItems = () =>
    lookup
      .producers()
      .filter((p) => p.isActive)
      .map((p) => ({ value: p.id, title: p.name }))

  const producers: RefConfig<Producer> = {
    key: 'producers',
    title: 'Производители',
    rowKey: (p) => p.id,
    load: async () => (await call(api.GET('/producers'))).items,
    columns: [
      { key: 'name', title: 'Название' },
      { key: 'country', title: 'Страна', value: (p) => countryName(p.countryCode) },
      { key: 'inn', title: 'ИНН' },
    ],
    fields: [
      { key: 'name', label: 'Название' },
      { key: 'countryCode', label: 'Страна', type: 'select', items: countryItems },
      {
        key: 'inn',
        label: 'ИНН',
        optional: true,
        hint: 'Для российских производителей, 10 или 12 цифр',
      },
    ],
    hasActive: true,
    toForm: (p) => ({ name: p.name, countryCode: p.countryCode, inn: p.inn ?? '' }),
    create: (v) =>
      call(
        api.POST('/producers', {
          body: { name: text(v.name), countryCode: text(v.countryCode), inn: optional(v.inn) },
        }),
      ),
    update: (p, v) =>
      call(
        api.PUT('/producers/{id}', {
          params: { path: { id: p.id } },
          body: {
            name: text(v.name),
            countryCode: text(v.countryCode),
            inn: optional(v.inn),
            isActive: Boolean(v.isActive),
          },
        }),
      ),
  }

  const brands: RefConfig<Brand> = {
    key: 'brands',
    title: 'Бренды',
    rowKey: (b) => b.id,
    load: async () => (await call(api.GET('/brands'))).items,
    columns: [
      { key: 'name', title: 'Бренд' },
      { key: 'producer', title: 'Производитель', value: (b) => producerName(b.producerId) },
    ],
    fields: [
      { key: 'name', label: 'Бренд' },
      { key: 'producerId', label: 'Производитель', type: 'select', items: producerItems },
    ],
    hasActive: true,
    toForm: (b) => ({ name: b.name, producerId: b.producerId }),
    create: (v) =>
      call(api.POST('/brands', { body: { name: text(v.name), producerId: text(v.producerId) } })),
    update: (b, v) =>
      call(
        api.PUT('/brands/{id}', {
          params: { path: { id: b.id } },
          body: {
            name: text(v.name),
            producerId: text(v.producerId),
            isActive: Boolean(v.isActive),
          },
        }),
      ),
  }

  const originRegions: RefConfig<OriginRegion> = {
    key: 'origin-regions',
    title: 'Регионы происхождения',
    rowKey: (r) => r.id,
    load: async () => (await call(api.GET('/origin-regions'))).items,
    columns: [
      { key: 'name', title: 'Регион' },
      { key: 'country', title: 'Страна', value: (r) => countryName(r.countryCode) },
    ],
    fields: [
      { key: 'name', label: 'Регион', hint: 'Например, Шампань, Коньяк, Кахетия' },
      { key: 'countryCode', label: 'Страна', type: 'select', items: countryItems },
    ],
    hasActive: true,
    toForm: (r) => ({ name: r.name, countryCode: r.countryCode }),
    create: (v) =>
      call(
        api.POST('/origin-regions', {
          body: { name: text(v.name), countryCode: text(v.countryCode) },
        }),
      ),
    update: (r, v) =>
      call(
        api.PUT('/origin-regions/{id}', {
          params: { path: { id: r.id } },
          body: {
            name: text(v.name),
            countryCode: text(v.countryCode),
            isActive: Boolean(v.isActive),
          },
        }),
      ),
  }

  const productKinds: RefConfig<ProductKind> = {
    key: 'product-kinds',
    title: 'Виды продукции',
    rowKey: (k) => k.code,
    load: async () => (await call(api.GET('/product-kinds'))).items,
    columns: [
      { key: 'code', title: 'Код' },
      { key: 'name', title: 'Вид продукции' },
      {
        key: 'egaisAccounting',
        title: 'Учёт в ЕГАИС',
        value: (k) => (k.egaisAccounting === 'marked' ? 'помарочный' : 'по количеству'),
      },
    ],
  }

  const volumes: RefConfig<Volume> = {
    key: 'volumes',
    title: 'Объёмы тары',
    rowKey: (v) => v.ml,
    load: async () => (await call(api.GET('/volumes'))).items,
    columns: [{ key: 'ml', title: 'Объём', value: (v) => formatVolume(v.ml) }],
    fields: [{ key: 'litres', label: 'Объём, л', createOnly: true, hint: 'Например, 0,7' }],
    hasActive: true,
    create: (v) =>
      call(
        api.POST('/volumes', {
          body: { ml: Math.round(Number(text(v.litres).replace(',', '.')) * 1000) },
        }),
      ),
    update: (v, values) =>
      call(
        api.PUT('/volumes/{ml}', {
          params: { path: { ml: v.ml } },
          body: { isActive: Boolean(values.isActive) },
        }),
      ),
  }

  const writeoffReasons: RefConfig<WriteoffReason> = {
    key: 'writeoff-reasons',
    title: 'Причины списания',
    rowKey: (r) => r.id,
    load: async () => (await call(api.GET('/writeoff-reasons'))).items,
    columns: [
      { key: 'name', title: 'Причина' },
      { key: 'egaisType', title: 'Тип списания в ЕГАИС' },
    ],
    fields: [
      { key: 'name', label: 'Причина' },
      {
        key: 'egaisType',
        label: 'Тип списания в ЕГАИС',
        type: 'select',
        items: () =>
          Object.entries(egaisWriteoffTypes).map(([value, hint]) => ({
            value,
            title: `${value} — ${hint}`,
          })),
      },
    ],
    hasActive: true,
    toForm: (r) => ({ name: r.name, egaisType: r.egaisType }),
    create: (v) =>
      call(
        api.POST('/writeoff-reasons', {
          body: { name: text(v.name), egaisType: text(v.egaisType) as EgaisWriteoffType },
        }),
      ),
    update: (r, v) =>
      call(
        api.PUT('/writeoff-reasons/{id}', {
          params: { path: { id: r.id } },
          body: {
            name: text(v.name),
            egaisType: text(v.egaisType) as EgaisWriteoffType,
            isActive: Boolean(v.isActive),
          },
        }),
      ),
  }

  const countries: RefConfig<Country> = {
    key: 'countries',
    title: 'Страны',
    rowKey: (c) => c.code,
    load: async () => (await call(api.GET('/countries'))).items,
    columns: [
      { key: 'code', title: 'Код ОКСМ' },
      { key: 'alpha2', title: 'Буквенный' },
      { key: 'name', title: 'Страна' },
    ],
  }

  return [producers, brands, originRegions, productKinds, volumes, writeoffReasons, countries]
}
