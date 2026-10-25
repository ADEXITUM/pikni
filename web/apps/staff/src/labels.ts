import type {
  DocumentKind,
  DocumentScanStatus,
  DocumentStatus,
  LocationKind,
  MarkStatus,
  ReceivingMode,
  Role,
  SaleScope,
  ZoneKind,
} from '@pikni/api'

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

export const zoneLabels: Record<ZoneKind, string> = {
  warehouse: 'Склад',
  backroom: 'Склад магазина',
  floor: 'Торговый зал',
}

export const documentKindLabels: Record<DocumentKind, string> = {
  opening: 'Ввод начальных остатков',
  floor_transfer: 'Передача в зал',
  receipt: 'Поступление',
  sale: 'Продажа',
  transfer: 'Перемещение',
  writeoff: 'Списание',
  storno: 'Сторно',
}

export const documentStatusLabels: Record<DocumentStatus, string> = {
  requested: 'Заявка',
  rejected: 'Отклонена',
  in_transit: 'В пути',
  pending_approval: 'На утверждении',
  reversed: 'Сторнирован',
  draft: 'Черновик',
  awaiting_egais: 'Ожидает ЕГАИС',
  posted: 'Проведён',
  egais_rejected: 'Отклонён ЕГАИС',
  cancelled: 'Отменён',
}

export const documentStatusColors: Record<DocumentStatus, string> = {
  requested: 'info',
  rejected: 'error',
  in_transit: 'warning',
  pending_approval: 'info',
  reversed: 'secondary',
  draft: 'secondary',
  awaiting_egais: 'warning',
  posted: 'success',
  egais_rejected: 'error',
  cancelled: 'secondary',
}

export const scanStatusLabels: Record<DocumentScanStatus, string> = {
  accepted: 'Засчитан',
  selected: 'Выбран товар',
  unknown: 'Не найден',
  foreign: 'Чужая марка',
  duplicate: 'Повтор',
  rejected: 'Не засчитан',
}

export const markStatusLabels: Record<MarkStatus, string> = {
  in_stock: 'На остатке',
  in_transit: 'В пути',
  reserved: 'Отложена для покупателя',
  sold: 'Продана',
  written_off: 'Списана',
  returned: 'Возвращена поставщику',
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

export const saleScopeLabels: Record<SaleScope, string> = {
  all: 'Вся алкогольная продукция',
  strong: 'Крепче 15 %',
}

// Режимы приёмки (СО-2).
export const receivingModeLabels: Record<ReceivingMode, string> = {
  document: 'по документу',
  boxes: 'по коробам',
  sample: 'выборочно',
  piece: 'поштучно',
}
