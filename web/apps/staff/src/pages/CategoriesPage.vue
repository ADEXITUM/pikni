<script setup lang="ts">
import { mdiFolderPlusOutline, mdiPencilOutline, mdiPlus } from '@mdi/js'
import type { Category, CategoryAttribute } from '@pikni/api'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, nextTick, ref, useTemplateRef, type ComponentPublicInstance } from 'vue'
import { useDisplay } from 'vuetify'

import {
  ancestors,
  attributeTypeLabels,
  maxCategoryDepth,
  schemaOf,
  treeOrder,
  type SchemaRow,
} from '@/categories'
import AttributeDialog from '@/components/AttributeDialog.vue'
import CategoryDialog from '@/components/CategoryDialog.vue'
import { categoriesQuery } from '@/queries'
import { useMe } from '@/session'

const queryClient = useQueryClient()
const { data: me } = useMe()
const canEdit = computed(() => me.value?.permissions.includes('refs.edit') ?? false)

const { data: tree, isPending, isError, error } = useQuery(categoriesQuery)
const categories = computed(() => tree.value?.categories ?? [])
const ordered = computed(() => treeOrder(categories.value))

const selectedId = ref<string>()
const selected = computed(
  () => categories.value.find((c) => c.id === selectedId.value) ?? ordered.value[0],
)

// На узком экране карточка стоит под деревом, поэтому после выбора к ней прокручиваем.
const { smAndDown } = useDisplay()
const card = useTemplateRef<ComponentPublicInstance>('card')
async function select(id: string) {
  selectedId.value = id
  if (!smAndDown.value) return
  await nextTick()
  card.value?.$el.scrollIntoView({ behavior: 'smooth' })
}
const path = computed(() => (selected.value ? ancestors(selected.value, categories.value) : []))
const schema = computed(() =>
  selected.value ? schemaOf(selected.value, categories.value, tree.value?.attributes ?? []) : [],
)

const nextSort = (sorts: number[]) => Math.max(0, ...sorts) + 10
const refresh = () => queryClient.invalidateQueries({ queryKey: categoriesQuery.queryKey })

const categoryOpen = ref(false)
const categoryTarget = ref<{ category?: Category; parent?: Category }>({})
const categorySort = computed(() =>
  nextSort(
    categories.value
      .filter((c) => c.parentId === categoryTarget.value.parent?.id)
      .map((c) => c.sort),
  ),
)
function openCategory(target: { category?: Category; parent?: Category }) {
  categoryTarget.value = target
  categoryOpen.value = true
}
function onCategorySaved(c: Category) {
  void select(c.id)
  refresh()
}

const attributeOpen = ref(false)
const attributeTarget = ref<CategoryAttribute>()
const attributeSort = computed(() =>
  nextSort(schema.value.filter((r) => !r.from).map((r) => r.attribute.sort)),
)
function openAttribute(a?: CategoryAttribute) {
  attributeTarget.value = a
  attributeOpen.value = true
}
// Унаследованную характеристику правят в той категории, где она заведена.
function onRow(row: SchemaRow) {
  if (canEdit.value && !row.from) openAttribute(row.attribute)
}

const values = (a: CategoryAttribute) =>
  a.options
    .filter((o) => !o.archived)
    .map((o) => o.value)
    .join(', ')
</script>

<template>
  <v-container>
    <div class="d-flex flex-wrap align-center ga-3 mb-2">
      <h1 class="text-headline-small flex-grow-1">Категории товаров</h1>
      <v-btn
        v-if="canEdit"
        color="primary"
        :prepend-icon="mdiPlus"
        data-test="category-add-root"
        @click="openCategory({})"
      >
        Категория
      </v-btn>
    </div>
    <p class="text-body-medium text-medium-emphasis mb-4">
      Не больше трёх уровней. Подкатегория получает характеристики всех категорий выше: у игристого
      есть цвет и содержание сахара из «Вина».
    </p>

    <v-alert v-if="isError" type="error" class="mb-4">{{ error?.message }}</v-alert>

    <v-row>
      <v-col cols="12" md="4">
        <v-card :loading="isPending">
          <v-list density="compact" nav>
            <v-list-item
              v-for="c in ordered"
              :key="c.id"
              :title="c.name"
              :active="c.id === selected?.id"
              :class="{ 'text-disabled': !c.isActive }"
              :style="{ paddingInlineStart: `${8 + (c.depth - 1) * 20}px` }"
              :data-test="`category-${c.name}`"
              @click="select(c.id)"
            >
              <template v-if="!c.isActive" #append>
                <v-chip size="x-small">в архиве</v-chip>
              </template>
            </v-list-item>
          </v-list>
        </v-card>
      </v-col>

      <v-col cols="12" md="8">
        <v-card v-if="selected" ref="card" data-test="category-card">
          <v-card-item>
            <v-card-subtitle v-if="path.length">
              {{ path.map((p) => p.name).join(' › ') }}
            </v-card-subtitle>
            <v-card-title class="d-flex align-center ga-2">
              {{ selected.name }}
              <v-chip v-if="!selected.isActive" size="small" color="error">в архиве</v-chip>
            </v-card-title>
            <template v-if="canEdit" #append>
              <v-btn
                :icon="mdiPencilOutline"
                variant="text"
                title="Изменить категорию"
                data-test="category-edit"
                @click="openCategory({ category: selected })"
              />
              <v-btn
                v-if="selected.depth < maxCategoryDepth && selected.isActive"
                :icon="mdiFolderPlusOutline"
                variant="text"
                title="Добавить подкатегорию"
                data-test="category-add-child"
                @click="openCategory({ parent: selected })"
              />
            </template>
          </v-card-item>

          <v-card-text>
            <div class="d-flex align-center mb-2">
              <div class="text-title-medium flex-grow-1">Характеристики</div>
              <v-btn
                v-if="canEdit"
                size="small"
                variant="tonal"
                :prepend-icon="mdiPlus"
                data-test="attr-add"
                @click="openAttribute()"
              >
                Добавить
              </v-btn>
            </div>
            <v-table v-if="schema.length" density="comfortable">
              <thead>
                <tr>
                  <th>Название</th>
                  <th>Тип</th>
                  <th>Значения</th>
                  <th>Поиск</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="row in schema"
                  :key="row.attribute.id"
                  :class="{
                    'pk-clickable': canEdit && !row.from,
                    'text-medium-emphasis': row.from,
                  }"
                  :data-test="`attr-${row.attribute.code}`"
                  @click="onRow(row)"
                >
                  <td class="pk-name">
                    {{ row.attribute.name }}
                    <v-chip v-if="!row.attribute.isActive" size="x-small" class="ml-1">
                      в архиве
                    </v-chip>
                    <div class="text-body-small text-no-wrap">
                      {{ row.attribute.code }}
                      <template v-if="row.from">
                        · из
                        <button type="button" class="pk-link" @click.stop="select(row.from.id)">
                          «{{ row.from.name }}»
                        </button>
                      </template>
                    </div>
                  </td>
                  <td class="text-no-wrap">
                    {{ attributeTypeLabels[row.attribute.type]
                    }}<template v-if="row.attribute.unit">, {{ row.attribute.unit }}</template>
                  </td>
                  <td class="text-body-medium">{{ values(row.attribute) }}</td>
                  <td class="text-no-wrap">
                    <v-chip v-if="row.attribute.filterable" size="x-small" class="mr-1">
                      фильтр
                    </v-chip>
                    <v-chip v-if="row.attribute.usedForAnalogs" size="x-small">аналоги</v-chip>
                  </td>
                </tr>
              </tbody>
            </v-table>
            <p v-else class="text-body-medium text-medium-emphasis">Характеристик нет</p>
          </v-card-text>
        </v-card>
      </v-col>
    </v-row>

    <CategoryDialog
      v-model="categoryOpen"
      v-bind="categoryTarget"
      :next-sort="categorySort"
      @saved="onCategorySaved"
    />
    <AttributeDialog
      v-if="selected"
      v-model="attributeOpen"
      :category-id="selected.id"
      :attribute="attributeTarget"
      :next-sort="attributeSort"
      @saved="refresh"
    />
  </v-container>
</template>

<style scoped>
.pk-clickable {
  cursor: pointer;
}

.pk-name {
  min-width: 180px;
}

.pk-link {
  padding: 0;
  border: 0;
  background: none;
  font: inherit;
  color: rgb(var(--v-theme-primary));
  text-decoration: underline;
  cursor: pointer;
}
</style>
