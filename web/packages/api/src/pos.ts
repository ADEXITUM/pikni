import createClient from 'openapi-fetch'

import type { components, paths } from './pos.gen'

type Schemas = components['schemas']

export type PosCatalog = Schemas['PosCatalog']
export type PosCheck = Schemas['PosCheck']
export type PosItem = Schemas['PosItem']
export type PosNewLine = Schemas['PosNewLine']
export type PosNewReceipt = Schemas['PosNewReceipt']
export type PosReceipt = Schemas['PosReceipt']
export type PosRefusal = Schemas['PosRefusal']
export type PosReservation = Schemas['PosReservation']
export type PosSession = Schemas['PosSession']
export type PosShift = Schemas['PosShift']
export type PosShiftState = Schemas['PosShiftState']
export type PosTime = Schemas['PosTime']
export type SaleWindow = Schemas['SaleWindow']

// Кассовый API (КС-6): им пользуется учебная касса и может пользоваться любая другая.
export const posApi = createClient<paths>({ baseUrl: '/api/pos/v1' })
