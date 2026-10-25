import { describe, expect, it } from 'vitest'

import { scalable } from '../qr'

describe('scalable', () => {
  it('добавляет viewBox и убирает фиксированный размер и заголовок XML', () => {
    const out = scalable(
      '<?xml version="1.0"?>\n<!DOCTYPE svg>\n<svg width="164" height="164" version="1.1" xmlns="http://www.w3.org/2000/svg"><path d="M0 0h4v4H0Z"/></svg>',
    )
    expect(out.startsWith('<svg viewBox="0 0 164 164"')).toBe(true)
    expect(out).not.toContain('width="164"')
    expect(out).toContain('shape-rendering="crispEdges"')
    expect(out).toContain('<path d="M0 0h4v4H0Z"/>')
  })
})
