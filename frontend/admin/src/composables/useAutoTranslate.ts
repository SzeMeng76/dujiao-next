import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { notifyError, notifySuccess } from '@/utils/notify'

export type LocalizedRecord = Record<string, string>

/**
 * 异步翻译：提交任务后轮询结果，避免长文本超时
 */
export function useAutoTranslate() {
  const { t } = useI18n()
  const translating = ref(false)

  const translateFields = async (fields: Record<string, LocalizedRecord>): Promise<boolean> => {
    const sourceEntries = Object.entries(fields)
      .map(([key, value]) => [key, (value['zh-CN'] || '').trim()] as const)
      .filter(([, text]) => text !== '')

    if (sourceEntries.length === 0) {
      notifyError(t('common.translate.noContent'))
      return false
    }

    const payload: Record<string, string> = {}
    sourceEntries.forEach(([key, text]) => {
      payload[key] = text
    })

    translating.value = true
    try {
      // 提交异步翻译任务
      const submitRes = await adminAPI.post('/settings/translation/jobs', { fields: payload })
      const jobId = submitRes.data?.data?.job_id

      if (!jobId) {
        throw new Error('Failed to submit translation job')
      }

      // 轮询任务状态
      const result = await pollTranslationJob(jobId)

      // 将结果写回原字段
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

      notifySuccess(t('common.translate.success'))
      return true
    } catch (err: any) {
      notifyError(err?.message || t('common.translate.failed'))
      return false
    } finally {
      translating.value = false
    }
  }

  const pollTranslationJob = async (
    jobId: string,
    maxWaitTime = 300000
  ): Promise<Record<string, Record<string, string>>> => {
    const startTime = Date.now()
    const pollInterval = 2000

    while (Date.now() - startTime < maxWaitTime) {
      const statusRes = await adminAPI.get(`/settings/translation/jobs/${jobId}`)
      const job = statusRes.data?.data

      if (job.status === 'completed') {
        return job.result || {}
      }

      if (job.status === 'failed') {
        throw new Error(job.error || 'Translation job failed')
      }

      // 仍在处理中，等待后继续
      await new Promise((resolve) => setTimeout(resolve, pollInterval))
    }

    throw new Error('Translation job timeout')
  }

  return { translating, translateFields }
}
