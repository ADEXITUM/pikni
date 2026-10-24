import { describe, expect, it } from 'vitest'

import { frameRect, videoRegion } from '../frame'

const phone = { width: 390, height: 520 }
const video = { width: 1920, height: 1080 }

describe('рамка видоискателя', () => {
  it('широкая рамка для PDF417 и штрихкодов, квадратная — для DataMatrix и QR', () => {
    const wide = frameRect(phone, 'wide')
    expect(wide.width / wide.height).toBeCloseTo(2.4)
    expect(wide.x + wide.width / 2).toBeCloseTo(phone.width / 2)

    const square = frameRect(phone, 'square')
    expect(square.width).toBe(square.height)
    expect(square.y + square.height / 2).toBeCloseTo(phone.height / 2)
  })

  it('рамка на экране переводится в пиксели кадра с учётом обрезки по object-fit: cover', () => {
    const frame = frameRect(phone, 'square')
    const r = videoRegion(video, phone, frame, 0)
    // Кадр 16:9 в портретном окне увеличен по высоте и обрезан по бокам.
    const scale = phone.height / video.height
    expect(r.width).toBeCloseTo(frame.width / scale)
    expect(r.x + r.width / 2).toBeCloseTo(video.width / 2)
    expect(r.y + r.height / 2).toBeCloseTo(video.height / 2)
  })

  it('запас вокруг рамки не выходит за границы кадра', () => {
    const view = { width: 1600, height: 900 }
    const r = videoRegion(video, view, { x: 0, y: 0, width: 1600, height: 900 })
    expect(r).toEqual({ x: 0, y: 0, width: 1920, height: 1080 })
  })
})
