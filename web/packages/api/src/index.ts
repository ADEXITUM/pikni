import createClient from 'openapi-fetch'

import type { components, paths } from './pikni.gen'

export type { Middleware } from 'openapi-fetch'

export type AssortmentItem = components['schemas']['AssortmentItem']
export type AttributeOption = components['schemas']['AttributeOption']
export type AttributeType = components['schemas']['AttributeType']
export type AuditAction = components['schemas']['AuditAction']
export type AuditEntry = components['schemas']['AuditEntry']
export type Barcode = components['schemas']['Barcode']
export type Brand = components['schemas']['Brand']
export type Category = components['schemas']['Category']
export type CategoryAttribute = components['schemas']['CategoryAttribute']
export type CategoryTree = components['schemas']['CategoryTree']
export type Container = components['schemas']['Container']
export type Country = components['schemas']['Country']
export type DayHours = components['schemas']['DayHours']
export type EgaisWriteoffType = components['schemas']['EgaisWriteoffType']
export type OriginRegion = components['schemas']['OriginRegion']
export type Producer = components['schemas']['Producer']
export type EgaisAccounting = components['schemas']['EgaisAccounting']
export type Product = components['schemas']['Product']
export type ProductKind = components['schemas']['ProductKind']
export type ProductLine = components['schemas']['ProductLine']
export type Variant = components['schemas']['Variant']
export type Volume = components['schemas']['Volume']
export type WriteoffReason = components['schemas']['WriteoffReason']
export type ImportReport = components['schemas']['ImportReport']
export type Location = components['schemas']['Location']
export type LocationUpdate = components['schemas']['LocationUpdate']
export type NewLocation = components['schemas']['NewLocation']
export type OpeningHours = components['schemas']['OpeningHours']
export type RuSubject = components['schemas']['RuSubject']
export type ZoneKind = components['schemas']['ZoneKind']
export type LocationKind = components['schemas']['LocationKind']
export type MinPrice = components['schemas']['MinPrice']
export type MinPriceGroup = components['schemas']['MinPriceGroup']
export type Me = components['schemas']['Me']
export type Permission = components['schemas']['Permission']
export type PriceKind = components['schemas']['PriceKind']
export type Problem = components['schemas']['Problem']
export type ReceivingMode = components['schemas']['ReceivingMode']
export type Role = components['schemas']['Role']
export type SaleBan = components['schemas']['SaleBan']
export type SaleCheck = components['schemas']['SaleCheck']
export type SaleRule = components['schemas']['SaleRule']
export type SaleScope = components['schemas']['SaleScope']
export type SaleVerdict = components['schemas']['SaleVerdict']
export type ScanKind = components['schemas']['ScanKind']
export type SessionInfo = components['schemas']['SessionInfo']
export type Supplier = components['schemas']['Supplier']
export type User = components['schemas']['User']

export const api = createClient<paths>({ baseUrl: '/api/v1' })

export class ApiError extends Error {
  constructor(readonly problem: Problem) {
    super(problem.detail ?? problem.title)
    this.name = 'ApiError'
  }
}
