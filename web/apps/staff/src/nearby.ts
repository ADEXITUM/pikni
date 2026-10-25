// Где искать товар: сотруднику точки сначала показывается его город, затем регион, затем вся сеть.
export type Scope = 'city' | 'region' | 'network'

export interface Place {
  city: string
  subjectCode: string
  lat?: number
  lon?: number
}

export function inScope(scope: Scope, here: Place | undefined, p: Place): boolean {
  if (!here || scope === 'network') return true
  return scope === 'city'
    ? p.city === here.city && p.subjectCode === here.subjectCode
    : p.subjectCode === here.subjectCode
}

// distanceKm — расстояние по прямой (гаверсинус); undefined, если у точки нет координат.
export function distanceKm(a?: Place, b?: Place): number | undefined {
  if (a?.lat === undefined || a.lon === undefined || b?.lat === undefined || b.lon === undefined)
    return undefined
  const rad = Math.PI / 180
  const dLat = (b.lat - a.lat) * rad
  const dLon = (b.lon - a.lon) * rad
  const h =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.sin(dLon / 2) ** 2
  return 12742 * Math.asin(Math.sqrt(h))
}

export function formatDistance(km?: number): string {
  if (km === undefined) return ''
  if (km < 1) return `${Math.round(km * 1000)} м`
  return `${km < 10 ? km.toFixed(1).replace('.', ',') : Math.round(km)} км`
}
