<script setup lang="ts">
import { mdiDotsHorizontal, mdiMagnify } from '@mdi/js'
import { listenWedge } from '@pikni/scanner'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useDisplay } from 'vuetify'

import LocationPicker from '@/components/LocationPicker.vue'
import PairingButton from '@/components/PairingButton.vue'
import PikniLogo from '@/components/PikniLogo.vue'
import UserMenu from '@/components/UserMenu.vue'
import { navItems } from '@/navigation'
import { onPairedScan, resumePairing } from '@/pairing'
import { canOpen, useMe } from '@/session'
import { useCurrentLocation } from '@/stores/currentLocation'

const router = useRouter()
const route = useRoute()
const { xs, mdAndUp } = useDisplay()
const { data: me } = useMe()
const current = useCurrentLocation()

watch(me, (m) => m && current.restore(m), { immediate: true })

const items = computed(() => {
  const user = me.value
  if (!user) return []
  return navItems.filter(
    (item) =>
      canOpen(user, router.resolve({ name: item.route })) &&
      (!item.forEditors || user.permissions.includes('refs.edit')),
  )
})
// В нижней панели помещается четыре кнопки: если разделов больше, последняя — «Ещё».
const bottomItems = computed(() => (items.value.length > 4 ? items.value.slice(0, 3) : items.value))
const moreItems = computed(() => items.value.slice(bottomItems.value.length))

// Поиск из шапки ведёт в каталог; скан ручным сканером на любом экране открывает «Сканер»
// с этим кодом. «Сканер», документы со сканированием и поля ввода обрабатывают скан сами.
const query = ref('')
function search() {
  const q = query.value.trim()
  if (!q) return
  query.value = ''
  void router.push({ name: 'products', query: { q } })
}
// Скан со смартфона-сканера на экране без сканирования — тоже в «Сканер».
let stops: (() => void)[] = []
onMounted(() => {
  const toScan = (code: string) => {
    if (!route.meta.scans && me.value) void router.push({ name: 'scan', query: { code } })
  }
  stops = [listenWedge({ ignoreFields: true, onScan: toScan }), onPairedScan(toScan)]
  void resumePairing()
})
onBeforeUnmount(() => stops.forEach((s) => s()))
</script>

<template>
  <v-app-bar>
    <template #prepend>
      <PikniLogo :height="28" class="ml-2 mr-3" />
    </template>
    <LocationPicker />
    <v-spacer />
    <v-text-field
      v-if="mdAndUp"
      v-model="query"
      :prepend-inner-icon="mdiMagnify"
      placeholder="Товар: название, артикул или штрихкод"
      density="compact"
      variant="solo-filled"
      flat
      hide-details
      class="pk-search mr-2"
      data-test="global-search"
      @keydown.enter="search"
    />
    <v-btn
      v-else
      :icon="mdiMagnify"
      :to="{ name: 'products' }"
      variant="text"
      aria-label="Поиск товара"
    />
    <template #append>
      <PairingButton v-if="!xs" />
      <UserMenu />
    </template>
  </v-app-bar>

  <!-- ПК — меню полностью, планшет — только значки, смартфон — нижняя панель. -->
  <v-navigation-drawer v-if="!xs" permanent :rail="!mdAndUp">
    <v-list nav>
      <v-list-item
        v-for="item in items"
        :key="item.route"
        :to="{ name: item.route }"
        :prepend-icon="item.icon"
        :title="item.title"
      />
    </v-list>
  </v-navigation-drawer>

  <v-bottom-navigation v-if="xs && items.length > 1">
    <v-btn v-for="item in bottomItems" :key="item.route" :to="{ name: item.route }">
      <v-icon :icon="item.icon" />
      <span>{{ item.title }}</span>
    </v-btn>
    <v-menu v-if="moreItems.length" location="top">
      <template #activator="{ props }">
        <v-btn v-bind="props" data-test="nav-more">
          <v-icon :icon="mdiDotsHorizontal" />
          <span>Ещё</span>
        </v-btn>
      </template>
      <v-list>
        <v-list-item
          v-for="item in moreItems"
          :key="item.route"
          :to="{ name: item.route }"
          :prepend-icon="item.icon"
          :title="item.title"
        />
      </v-list>
    </v-menu>
  </v-bottom-navigation>
</template>

<style scoped>
.pk-search {
  max-width: 420px;
}
</style>
