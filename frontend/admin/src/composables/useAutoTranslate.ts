import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { notifyError, notifySuccess } from '@/utils/notify'

export type LocalizedRecord = Record<string, string>

// Fields with source text longer than this are translated in their own
// request instead of being batched with short fields. A single long field
// (e.g. a product's rich-text "content") can take much longer for the model
// to translate than a title or SEO keyword; grouping it separately means the
// short fields still come back quickly instead of waiting on the slowest one.
const LONG_FIELD_CHARS = 300

const notifyErrorIfNeeded = (err: unknown, fallback: string) => {
  const known = err as Error & { __notified?: boolean }
  if (known?.__notified) return
  notifyError(known?.message || fallback)
}

/**
 * Shared "AI 翻译" helper: reads each field's zh-CN value, calls the backend
 * translate endpoint, and writes zh-TW/en-US back into the same reactive
 * LocalizedRecord objects so callers don't need to handle the response shape.
 *
 * Fields are grouped by source length and translated concurrently: short
 * fields (titles, keywords, ...) share one request and return fast; each
 * long field (rich-text content, ...) gets its own request so it doesn't
 * hold up the others.
 */
export function useAutoTranslate() {
  const { t } = useI18n()
  const translating = ref(false)

  const translateGroup = async (
    fields: Record<string, LocalizedRecord>,
    entries: ReadonlyArray<readonly [string, string]>
  ): Promise<boolean> => {
    try {
      const payload: Record<string, string> = {}
      entries.forEach(([key, text]) => {
        payload[key] = text
      })
      const res = await adminAPI.translateFields(payload)
      const result = (res.data?.data?.fields || {}) as Record<string, Record<string, string>>
      entries.forEach(([key]) => {
        const translated = result[key]
        const target = fields[key]
        if (!translated || !target) return
        if (typeof translated['zh-TW'] === 'string' && translated['zh-TW'] !== '') {
          target['zh-TW'] = translated['zh-TW']
        }
        if (typeof translated['en-US'] === 'string' && translated['en-US'] !== '') {
          target['en-US'] = translated['en-US']
        }
      })
      return true
    } catch (err) {
      notifyErrorIfNeeded(err, t('common.translate.failed'))
      return false
    }
  }

  const translateFields = async (fields: Record<string, LocalizedRecord>): Promise<boolean> => {
    const sourceEntries = Object.entries(fields)
      .map(([key, value]) => [key, (value['zh-CN'] || '').trim()] as const)
      .filter(([, text]) => text !== '')

    if (sourceEntries.length === 0) {
      notifyError(t('common.translate.noContent'))
      return false
    }

    const shortEntries = sourceEntries.filter(([, text]) => text.length <= LONG_FIELD_CHARS)
    const longEntries = sourceEntries.filter(([, text]) => text.length > LONG_FIELD_CHARS)
    const groups: Array<ReadonlyArray<readonly [string, string]>> = [
      ...(shortEntries.length > 0 ? [shortEntries] : []),
      ...longEntries.map((entry) => [entry] as const),
    ]

    translating.value = true
    try {
      const results = await Promise.all(groups.map((entries) => translateGroup(fields, entries)))
      const allOk = results.every(Boolean)
      if (allOk) {
        notifySuccess(t('common.translate.success'))
      }
      return allOk
    } finally {
      translating.value = false
    }
  }

  return { translating, translateFields }
}
