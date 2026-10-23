import {
  api,
  ApiError,
  type Brand,
  type Country,
  type EgaisWriteoffType,
  type MinPrice,
  type OriginRegion,
  type Problem,
  type Producer,
  type ProductKind,
  type ProductLine,
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
  inputType?: 'date' | 'number'
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
  // Удалить можно только то, что ещё не использовалось: например, будущую редакцию МРЦ.
  remove?(row: Row): Promise<unknown>
  canRemove?(row: Row): boolean
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
  brands: () => Brand[]
  kinds: () => ProductKind[]
}): RefConfig<object>[] {
  const countryName = (code: string) =>
    lookup.countries().find((c) => c.code === code)?.name ?? code
  const producerName = (id: string) => lookup.producers().find((p) => p.id === id)?.name ?? '…'
  const brandName = (id: string) => lookup.brands().find((b) => b.id === id)?.name ?? '…'
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

  const productLines: RefConfig<ProductLine> = {
    key: 'product-lines',
    title: 'Линейки',
    rowKey: (l) => l.id,
    load: async () => (await call(api.GET('/product-lines'))).items,
    columns: [
      { key: 'name', title: 'Линейка' },
      { key: 'brand', title: 'Бренд', value: (l) => brandName(l.brandId) },
    ],
    fields: [
      { key: 'name', label: 'Линейка', hint: 'Позиции бренда, которые различаются разновидностью' },
      {
        key: 'brandId',
        label: 'Бренд',
        type: 'select',
        createOnly: true,
        items: () =>
          lookup
            .brands()
            .filter((b) => b.isActive)
            .map((b) => ({ value: b.id, title: `${b.name} (${producerName(b.producerId)})` })),
      },
    ],
    hasActive: true,
    toForm: (l) => ({ name: l.name, brandId: l.brandId }),
    create: (v) =>
      call(api.POST('/product-lines', { body: { name: text(v.name), brandId: text(v.brandId) } })),
    update: (l, v) =>
      call(
        api.PUT('/product-lines/{id}', {
          params: { path: { id: l.id } },
          body: { name: text(v.name), isActive: Boolean(v.isActive) },
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

  const number = (v: unknown) => (text(v) === '' ? undefined : Number(text(v).replace(',', '.')))
  const today = new Date().toLocaleDateString('sv')
  const minPrices: RefConfig<MinPrice> = {
    key: 'min-prices',
    title: 'МРЦ',
    rowKey: (r) => r.id,
    load: async () => (await call(api.GET('/min-prices'))).items,
    columns: [
      {
        key: 'kindCode',
        title: 'Вид продукции',
        value: (r) =>
          `${r.kindCode} — ${lookup.kinds().find((k) => k.code === r.kindCode)?.name ?? ''}`,
      },
      {
        key: 'validFrom',
        title: 'Действует с',
        value: (r) => new Date(`${r.validFrom}T00:00`).toLocaleDateString('ru'),
      },
      {
        key: 'price',
        title: 'Цена',
        value: (r) =>
          `${r.price.toLocaleString('ru', { style: 'currency', currency: 'RUB' })} за ${formatVolume(r.volumeMl)}` +
          (r.abv ? ` при ${r.abv} %` : ''),
      },
      { key: 'abvOver', title: 'Крепче', value: (r) => (r.abvOver ? `${r.abvOver} %` : '') },
      { key: 'source', title: 'Основание' },
    ],
    fields: [
      {
        key: 'kindCode',
        label: 'Вид продукции',
        type: 'select',
        items: () => lookup.kinds().map((k) => ({ value: k.code, title: `${k.code} — ${k.name}` })),
      },
      { key: 'validFrom', label: 'Действует с', inputType: 'date' },
      { key: 'price', label: 'Цена, ₽', hint: 'За эталонный объём' },
      { key: 'volumeMl', label: 'Эталонный объём, мл', hint: 'Обычно 500' },
      {
        key: 'abv',
        label: 'Эталонная крепость, %',
        optional: true,
        hint: 'Если указана, цена пересчитывается и по крепости',
      },
      {
        key: 'abvOver',
        label: 'Только крепче, %',
        optional: true,
        hint: 'Например, 28 — для продукции крепче 28 %',
      },
      { key: 'source', label: 'Основание', hint: 'Приказ Минфина России' },
    ],
    create: (v) =>
      call(
        api.POST('/min-prices', {
          body: {
            kindCode: text(v.kindCode),
            validFrom: text(v.validFrom),
            price: number(v.price) ?? 0,
            volumeMl: number(v.volumeMl) ?? 0,
            abv: number(v.abv),
            abvOver: number(v.abvOver),
            source: text(v.source),
          },
        }),
      ),
    remove: (r) => call(api.DELETE('/min-prices/{id}', { params: { path: { id: r.id } } })),
    canRemove: (r) => r.validFrom > today,
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

  return [
    producers,
    brands,
    productLines,
    originRegions,
    productKinds,
    volumes,
    writeoffReasons,
    minPrices,
    countries,
  ]
}
