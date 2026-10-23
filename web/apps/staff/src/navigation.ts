import {
  mdiAccountGroupOutline,
  mdiBookshelf,
  mdiClockAlertOutline,
  mdiHistory,
  mdiStorefrontOutline,
} from '@mdi/js'

// Разделы меню по порядку. Право берётся из meta.permission маршрута, поэтому раздел
// сам появляется только у тех, кому он доступен.
export const navItems = [
  { title: 'Точки сети', icon: mdiStorefrontOutline, route: 'locations' },
  { title: 'Правила продажи', icon: mdiClockAlertOutline, route: 'sale-rules' },
  { title: 'Справочники', icon: mdiBookshelf, route: 'refs' },
  { title: 'Сотрудники', icon: mdiAccountGroupOutline, route: 'users' },
  { title: 'Журнал действий', icon: mdiHistory, route: 'audit' },
]
