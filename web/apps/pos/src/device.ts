const keyPattern = /^pos_[a-z2-7]+_[A-Za-z0-9_-]+$/

export function validKey(key: string): boolean {
  return keyPattern.test(key.trim())
}

// keyFromHash достаёт ключ из ссылки регистрации …/pos/#key=pos_… (QR-код в «Кассах»).
export function keyFromHash(hash: string): string | null {
  const key = new URLSearchParams(hash.replace(/^#/, '')).get('key')
  return key && validKey(key) ? key.trim() : null
}
