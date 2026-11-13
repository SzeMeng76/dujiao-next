import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { notifyError, notifySuccess } from '@/utils/notify'

export type LocalizedRecord = Record<string, string>

// 同时进行中的翻译任务上限。一个商品可能有十几个字段（标题、描述、正文、
// 每个 SKU 的规格…），若每个字段都立刻并发提交，一次点击就会同时打十几次
// 上游 AI 接口，很容易触发限流。这里用一个小池子把并发压住。
const MAX_CONCURRENT_JOBS = 2

/**
 * 异步翻译：按字段提交 job，但并发数受限。
 * 每个字段翻译完成后立即写回，不必等所有字段完成；
 * 单个字段失败也不影响其他字段。
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

    // 简单的并发池：始终保持最多 MAX_CONCURRENT_JOBS 个任务在跑。
    let cursor = 0
    const worker = async () => {
      while (cursor < sourceEntries.length) {
        const entry = sourceEntries[cursor++]
        if (!entry) break
        const [key, text] = entry
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
      }
    }

    try {
      const workers = Array.from(
        { length: Math.min(MAX_CONCURRENT_JOBS, sourceEntries.length) },
        () => worker()
      )
      await Promise.all(workers)

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
    // 轮询退避：一开始密一点好拿到快速完成的结果，之后逐步放宽，
    // 避免长任务期间每个 job 每 5 秒打一次接口。
    let pollInterval = 1000
    const maxPollInterval = 5000

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
      pollInterval = Math.min(pollInterval * 2, maxPollInterval)
    }

    throw new Error('Translation job timeout')
  }

  return { translating, translateFields }
}
