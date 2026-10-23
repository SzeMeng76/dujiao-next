import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { notifyError, notifySuccess } from '@/utils/notify'

export type LocalizedRecord = Record<string, string>

/**
 * 异步翻译：每个字段单独提交一个 job 并行处理，避免长文本超时。
 * 每个字段翻译完成后立即写回，不必等所有字段完成。
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

    translating.value = true
    let anySuccess = false
    let anyFail = false

    try {
      // 每个字段单独提交一个 job，并行执行
      await Promise.all(
        sourceEntries.map(async ([key, text]) => {
          try {
            const submitRes = await adminAPI.submitTranslationJob({ [key]: text })
            const jobId = submitRes.data?.data?.job_id
            if (!jobId) throw new Error('Failed to submit translation job')

            const result = await pollTranslationJob(jobId)
            const translated = result[key]
            const target = fields[key]

            if (translated && target) {
              if (typeof translated['zh-TW'] === 'string' && translated['zh-TW'] !== '') {
                target['zh-TW'] = translated['zh-TW']
              }
              if (typeof translated['en-US'] === 'string' && translated['en-US'] !== '') {
                target['en-US'] = translated['en-US']
              }
              anySuccess = true
            }
          } catch (err: any) {
            console.error(`translate field [${key}] failed:`, err?.message)
            anyFail = true
          }
        })
      )

      if (anySuccess && !anyFail) {
        notifySuccess(t('common.translate.success'))
      } else if (anySuccess && anyFail) {
        notifySuccess(t('common.translate.partialSuccess', '部分字段翻译成功'))
      } else {
        notifyError(t('common.translate.failed'))
      }

      return anySuccess
    } finally {
      translating.value = false
    }
  }

  const pollTranslationJob = async (
    jobId: string,
    maxWaitTime = 1200000
  ): Promise<Record<string, Record<string, string>>> => {
    const startTime = Date.now()
    const pollInterval = 5000

    while (Date.now() - startTime < maxWaitTime) {
      const statusRes = await adminAPI.getTranslationJobStatus(jobId)
      const job = statusRes.data?.data

      if (job.status === 'completed') {
        return job.result || {}
      }

      if (job.status === 'failed') {
        throw new Error(job.error || 'Translation job failed')
      }

      await new Promise((resolve) => setTimeout(resolve, pollInterval))
    }

    throw new Error('Translation job timeout')
  }

  return { translating, translateFields }
}
