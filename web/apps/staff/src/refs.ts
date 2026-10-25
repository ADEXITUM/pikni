import {
  api,
  ApiError,
  type Brand,
  type Country,
  type EgaisWriteoffType,
  type MinPrice,
  type MinPriceGroup,
  type OriginRegion,
  type Problem,
  type Producer,
  type ProductKind,
  type ProductLine,
  type ReceivingMode,
  type Supplier,
  type Volume,
  type WriteoffReason,
} from '@pikni/api'

import { receivingModeLabels } from '@/labels'

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
  // Значение в форме новой записи.
  initial?: string
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

function supplierBody(v: RefValues) {
  return {
    name: text(v.name),
    inn: text(v.inn),
    kpp: optional(v.kpp),
    fsrarId: optional(v.fsrarId),
    licenseNo: optional(v.licenseNo),
    licenseValidTo: optional(v.licenseValidTo),
    defaultReceivingMode: text(v.defaultReceivingMode) as ReceivingMode,
    contacts: { person: optional(v.person), phone: optional(v.phone), email: optional(v.email) },
  }
}

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
  minPriceGroups: () => MinPriceGroup[]
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
      { key: 'minPriceGroup', title: 'Группа МРЦ', value: (k) => k.minPriceGroup ?? '' },
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
      { key: 'photo', title: 'Фото', value: (r) => (r.photoRequired ? 'нужно' : '') },
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
      {
        key: 'photoRequired',
        label: 'Фотография при списании',
        type: 'select',
        initial: 'no',
        items: () => [
          { value: 'no', title: 'не нужна' },
          { value: 'yes', title: 'нужна — например, при бое' },
        ],
      },
    ],
    hasActive: true,
    toForm: (r) => ({
      name: r.name,
      egaisType: r.egaisType,
      photoRequired: r.photoRequired ? 'yes' : 'no',
    }),
    create: (v) =>
      call(
        api.POST('/writeoff-reasons', {
          body: {
            name: text(v.name),
            egaisType: text(v.egaisType) as EgaisWriteoffType,
            photoRequired: v.photoRequired === 'yes',
          },
        }),
      ),
    update: (r, v) =>
      call(
        api.PUT('/writeoff-reasons/{id}', {
          params: { path: { id: r.id } },
          body: {
            name: text(v.name),
            egaisType: text(v.egaisType) as EgaisWriteoffType,
            photoRequired: v.photoRequired === 'yes',
            isActive: Boolean(v.isActive),
          },
        }),
      ),
  }

  const number = (v: unknown) => (text(v) === '' ? undefined : Number(text(v).replace(',', '.')))
  const today = new Date().toLocaleDateString('sv')
  // Полоса крепости как в приказе Минфина: «свыше 39 до 40 %».
  const abvBand = (r: MinPrice) =>
    !r.abvFrom && r.abvTo === undefined
      ? 'любая'
      : `свыше ${r.abvFrom}` + (r.abvTo === undefined ? ' %' : ` до ${r.abvTo} %`)
  const minPrices: RefConfig<MinPrice> = {
    key: 'min-prices',
    title: 'МРЦ',
    rowKey: (r) => r.id,
    load: async () => (await call(api.GET('/min-prices'))).items,
    columns: [
      { key: 'groupName', title: 'Группа' },
      {
        key: 'validFrom',
        title: 'Действует с',
        value: (r) => new Date(`${r.validFrom}T00:00`).toLocaleDateString('ru'),
      },
      { key: 'abvFrom', title: 'Крепость', value: abvBand },
      {
        key: 'price',
        title: 'Цена',
        value: (r) =>
          `${r.price.toLocaleString('ru', { style: 'currency', currency: 'RUB' })} за ${formatVolume(r.volumeMl)}`,
      },
      { key: 'source', title: 'Основание' },
    ],
    fields: [
      {
        key: 'groupCode',
        label: 'Группа продукции',
        type: 'select',
        items: () => lookup.minPriceGroups().map((g) => ({ value: g.code, title: g.name })),
      },
      { key: 'validFrom', label: 'Действует с', inputType: 'date' },
      {
        key: 'abvFrom',
        label: 'Крепость свыше, %',
        hint: '0 — цена не зависит от крепости',
        initial: '0',
      },
      {
        key: 'abvTo',
        label: 'До … включительно, %',
        optional: true,
        hint: 'Пусто — без верхней границы',
      },
      { key: 'price', label: 'Цена, ₽', hint: 'За эталонный объём' },
      {
        key: 'volumeMl',
        label: 'Эталонный объём, мл',
        hint: '500; у игристого — 750',
        initial: '500',
      },
      {
        key: 'samePriceOverMl',
        label: 'Как эталонный объём, если тара свыше, мл',
        optional: true,
        hint: 'У водки 375: тара свыше 0,375 до 0,5 л стоит как 0,5 л',
      },
      { key: 'source', label: 'Основание', hint: 'Приказ Минфина России' },
    ],
    create: (v) =>
      call(
        api.POST('/min-prices', {
          body: {
            groupCode: text(v.groupCode),
            validFrom: text(v.validFrom),
            abvFrom: number(v.abvFrom) ?? 0,
            abvTo: number(v.abvTo),
            price: number(v.price) ?? 0,
            volumeMl: number(v.volumeMl) ?? 0,
            samePriceOverMl: number(v.samePriceOverMl),
            source: text(v.source),
          },
        }),
      ),
    remove: (r) => call(api.DELETE('/min-prices/{id}', { params: { path: { id: r.id } } })),
    canRemove: (r) => r.validFrom > today,
  }

  const suppliers: RefConfig<Supplier> = {
    key: 'suppliers',
    title: 'Поставщики',
    rowKey: (s) => s.id,
    load: async () => (await call(api.GET('/suppliers'))).items,
    columns: [
      { key: 'name', title: 'Поставщик' },
      { key: 'inn', title: 'ИНН' },
      { key: 'kpp', title: 'КПП' },
      { key: 'fsrarId', title: 'ФСРАР ID' },
      {
        key: 'defaultReceivingMode',
        title: 'Приёмка',
        value: (s) => receivingModeLabels[s.defaultReceivingMode],
      },
    ],
    fields: [
      { key: 'name', label: 'Название' },
      { key: 'inn', label: 'ИНН', hint: '10 цифр у организации, 12 — у предпринимателя' },
      { key: 'kpp', label: 'КПП', optional: true, hint: 'У предпринимателя нет' },
      { key: 'fsrarId', label: 'ФСРАР ID', optional: true, hint: '12 цифр' },
      { key: 'licenseNo', label: 'Номер лицензии', optional: true },
      { key: 'licenseValidTo', label: 'Лицензия действует до', optional: true, inputType: 'date' },
      {
        key: 'defaultReceivingMode',
        label: 'Режим приёмки по умолчанию',
        type: 'select',
        initial: 'piece',
        items: () =>
          Object.entries(receivingModeLabels).map(([value, title]) => ({ value, title })),
      },
      { key: 'person', label: 'Контактное лицо', optional: true },
      { key: 'phone', label: 'Телефон', optional: true },
      { key: 'email', label: 'Эл. почта', optional: true },
    ],
    hasActive: true,
    toForm: (s) => ({
      name: s.name,
      inn: s.inn,
      kpp: s.kpp ?? '',
      fsrarId: s.fsrarId ?? '',
      licenseNo: s.licenseNo ?? '',
      licenseValidTo: s.licenseValidTo ?? '',
      defaultReceivingMode: s.defaultReceivingMode,
      person: s.contacts.person ?? '',
      phone: s.contacts.phone ?? '',
      email: s.contacts.email ?? '',
    }),
    create: (v) => call(api.POST('/suppliers', { body: supplierBody(v) })),
    update: (s, v) =>
      call(
        api.PUT('/suppliers/{id}', {
          params: { path: { id: s.id } },
          body: { ...supplierBody(v), isActive: Boolean(v.isActive) },
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

  return [
    suppliers,
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
