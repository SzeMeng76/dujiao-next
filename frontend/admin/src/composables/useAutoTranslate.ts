import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { notifyError, notifySuccess } from '@/utils/notify'

export type LocalizedRecord = Record<string, string>

const notifyErrorIfNeeded = (err: unknown, fallback: string) => {
  const known = err as Error & { __notified?: boolean }
  if (known?.__notified) return
  notifyError(known?.message || fallback)
}

/**
 * Shared "AI 翻译" helper: reads each field's zh-CN value, calls the backend
 * translate endpoint, and writes zh-TW/en-US back into the same reactive
 * LocalizedRecord objects so callers don't need to handle the response shape.
 */
export function useAutoTranslate() {
  const { t } = useI18n()
  const translating = ref(false)

  const translateFields = async (fields: Record<string, LocalizedRecord>): Promise<boolean> => {
    const sourceEntries = Object.entries(fields)
      .map(([key, value]) => [key, (value['zh-CN'] || '').trim()] as const)
      .filter(([, text]) => text !== '')

    if (sourceEntries.length === 0) {
      notifyError(t('admin.common.translate.noContent'))
      return false
    }

    translating.value = true
    try {
      const payload: Record<string, string> = {}
      sourceEntries.forEach(([key, text]) => {
        payload[key] = text
      })
      const res = await adminAPI.translateFields(payload)
      const result = (res.data?.data?.fields || {}) as Record<string, Record<string, string>>
      sourceEntries.forEach(([key]) => {
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
      notifySuccess(t('admin.common.translate.success'))
      return true
    } catch (err) {
      notifyErrorIfNeeded(err, t('admin.common.translate.failed'))
      return false
    } finally {
      translating.value = false
    }
  }

  return { translating, translateFields }
}
