export type FrameShape = 'wide' | 'square'

export interface Size {
  width: number
  height: number
}

export interface Rect extends Size {
  x: number
  y: number
}

// frameRect — рамка видоискателя в координатах экрана. Широкая — для PDF417 марок старого
// образца и линейных штрихкодов, квадратная — для DataMatrix и QR.
export function frameRect(view: Size, shape: FrameShape): Rect {
  const width = shape === 'wide' ? view.width * 0.86 : Math.min(view.width, view.height) * 0.62
  const height = shape === 'wide' ? Math.min(width / 2.4, view.height * 0.5) : width
  return { x: (view.width - width) / 2, y: (view.height - height) / 2, width, height }
}

// videoRegion переводит рамку с экрана в пиксели кадра. Видео показано с object-fit: cover —
// кадр масштабирован до заполнения и обрезан по центру. Распознаётся немного больше рамки
// (pad с каждой стороны), чтобы код, чуть вышедший за уголки, всё равно читался.
export function videoRegion(video: Size, view: Size, frame: Rect, pad = 0.08): Rect {
  const scale = Math.max(view.width / video.width, view.height / video.height)
  const offsetX = (video.width * scale - view.width) / 2
  const offsetY = (video.height * scale - view.height) / 2
  const padX = frame.width * pad
  const padY = frame.height * pad
  const x = Math.max(0, (frame.x - padX + offsetX) / scale)
  const y = Math.max(0, (frame.y - padY + offsetY) / scale)
  return {
    x,
    y,
    width: Math.min(video.width - x, (frame.width + 2 * padX) / scale),
    height: Math.min(video.height - y, (frame.height + 2 * padY) / scale),
  }
}
