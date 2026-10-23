import type { AttributeType, Category, CategoryAttribute } from '@pikni/api'

export const maxCategoryDepth = 3

export const attributeTypeLabels: Record<AttributeType, string> = {
  enum: 'список',
  number: 'число',
  text: 'текст',
  bool: 'да/нет',
}

// Сервер отдаёт категории по глубине и порядку, поэтому соседи уже отсортированы.
export function treeOrder(categories: Category[]): Category[] {
  const children = new Map<string | undefined, Category[]>()
  for (const c of categories) children.set(c.parentId, [...(children.get(c.parentId) ?? []), c])
  const ordered: Category[] = []
  const walk = (parentId?: string) => {
    for (const c of children.get(parentId) ?? []) {
      ordered.push(c)
      walk(c.id)
    }
  }
  walk()
  return ordered
}

// ancestors возвращает предков от корня к родителю.
export function ancestors(category: Category, categories: Category[]): Category[] {
  const byId = new Map(categories.map((c) => [c.id, c]))
  const path: Category[] = []
  for (let p = byId.get(category.parentId ?? ''); p; p = byId.get(p.parentId ?? '')) path.unshift(p)
  return path
}

// categoryItems — варианты для выбора категории с полным путём: «Вино › Игристое». Архивные
// не предлагаются, кроме уже выбранной.
export function categoryItems(categories: Category[], current?: string | null) {
  return treeOrder(categories)
    .filter((c) => c.isActive || c.id === current)
    .map((c) => ({
      value: c.id,
      title: [...ancestors(c, categories), c].map((x) => x.name).join(' › '),
    }))
}

export interface SchemaRow {
  attribute: CategoryAttribute
  from?: Category
}

// schemaOf — полная схема характеристик категории: сначала унаследованные от предков,
// затем собственные (у них from не задан).
export function schemaOf(
  category: Category,
  categories: Category[],
  attributes: CategoryAttribute[],
): SchemaRow[] {
  const own = (c: Category) => attributes.filter((a) => a.categoryId === c.id)
  return [
    ...ancestors(category, categories).flatMap((from) =>
      own(from).map((attribute) => ({ attribute, from })),
    ),
    ...own(category).map((attribute) => ({ attribute })),
  ]
}
