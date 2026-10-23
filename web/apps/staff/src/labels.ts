import type { LocationKind, Role, ZoneKind } from '@pikni/api'

export const roleLabels: Record<Role, string> = {
  admin: 'Администратор',
  manager: 'Руководитель',
  accountant: 'Бухгалтер',
  store_manager: 'Управляющий магазином',
  storekeeper: 'Кладовщик',
  cashier: 'Продавец-кассир',
}

// Повторяет auth.networkRoles на сервере: этим ролям точки не назначаются.
export const networkRoles: ReadonlySet<Role> = new Set<Role>(['admin', 'manager', 'accountant'])

export const locationKindLabels: Record<LocationKind, string> = {
  warehouse: 'Склад',
  store: 'Магазин',
}

// Зона соответствует регистру ЕГАИС, где учитывается продукция.
export const zoneRegisters: Record<ZoneKind, string> = {
  warehouse: 'регистр 1 ЕГАИС',
  backroom: 'регистр 1 ЕГАИС',
  floor: 'регистр 2 ЕГАИС',
}

export const russianTimezones = [
  { value: 'Europe/Kaliningrad', title: 'Калининград (UTC+2)' },
  { value: 'Europe/Moscow', title: 'Москва (UTC+3)' },
  { value: 'Europe/Samara', title: 'Самара (UTC+4)' },
  { value: 'Asia/Yekaterinburg', title: 'Екатеринбург (UTC+5)' },
  { value: 'Asia/Omsk', title: 'Омск (UTC+6)' },
  { value: 'Asia/Novosibirsk', title: 'Новосибирск (UTC+7)' },
  { value: 'Asia/Krasnoyarsk', title: 'Красноярск (UTC+7)' },
  { value: 'Asia/Irkutsk', title: 'Иркутск (UTC+8)' },
  { value: 'Asia/Yakutsk', title: 'Якутск, Благовещенск (UTC+9)' },
  { value: 'Asia/Vladivostok', title: 'Владивосток, Хабаровск (UTC+10)' },
  { value: 'Asia/Magadan', title: 'Магадан (UTC+11)' },
  { value: 'Asia/Kamchatka', title: 'Камчатка (UTC+12)' },
]
