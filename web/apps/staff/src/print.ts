import { ApiError, type Problem } from '@pikni/api'

// openPdf открывает PDF, который сервер собрал по POST-запросу (ценники, наклейки), в новой
// вкладке — оттуда его печатают. Вкладка открывается сразу, чтобы браузер не счёл её всплывающим
// окном, и получает адрес, когда файл готов.
// openapi-fetch описывает двоичный ответ строкой, хотя с parseAs: 'blob' приходит Blob.
export async function openPdf(request: Promise<{ data?: unknown; error?: Problem }>) {
  const tab = window.open('', '_blank')
  try {
    const { data, error } = await request
    if (error || !data)
      throw new ApiError(error ?? { type: 'about:blank', title: 'Файл не получен', status: 500 })
    const url = URL.createObjectURL(data as Blob)
    if (tab) tab.location.href = url
    else window.location.href = url
    setTimeout(() => URL.revokeObjectURL(url), 60_000)
  } catch (e) {
    tab?.close()
    throw e
  }
}
