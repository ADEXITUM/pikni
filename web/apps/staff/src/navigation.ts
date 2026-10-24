import {
  mdiAccountGroupOutline,
  mdiBarcodeScan,
  mdiBookshelf,
  mdiBottleWineOutline,
  mdiClockAlertOutline,
  mdiFileTreeOutline,
  mdiHistory,
  mdiStorefrontOutline,
} from '@mdi/js'

// Разделы меню по порядку. Право берётся из meta.permission маршрута, поэтому раздел
// сам появляется только у тех, кому он доступен. forEditors — служебные справочники: в меню
// они у тех, кто их ведёт, остальным открываются по ссылке, например из карточки точки.
export const navItems: { title: string; icon: string; route: string; forEditors?: true }[] = [
  { title: 'Сканер', icon: mdiBarcodeScan, route: 'scan' },
  { title: 'Товары', icon: mdiBottleWineOutline, route: 'products' },
  { title: 'Точки сети', icon: mdiStorefrontOutline, route: 'locations' },
  { title: 'Правила продажи', icon: mdiClockAlertOutline, route: 'sale-rules', forEditors: true },
  { title: 'Категории', icon: mdiFileTreeOutline, route: 'categories', forEditors: true },
  { title: 'Справочники', icon: mdiBookshelf, route: 'refs', forEditors: true },
  { title: 'Сотрудники', icon: mdiAccountGroupOutline, route: 'users' },
  { title: 'Журнал действий', icon: mdiHistory, route: 'audit' },
]
