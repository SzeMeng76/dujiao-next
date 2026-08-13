<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { notifyError, notifySuccess } from '@/utils/notify'

const { t } = useI18n()

interface TranslationData {
  enabled: boolean
  api_key: string
  has_api_key: boolean
  base_url: string
  model: string
}

const props = defineProps<{
  data: TranslationData
}>()

const emit = defineEmits<{
  saved: []
}>()

const submitting = ref(false)

const form = reactive({
  enabled: false,
  api_key: '',
  has_api_key: false,
  base_url: '',
  model: '',
})

const syncFromProps = () => {
  form.enabled = props.data.enabled
  form.api_key = ''
  form.has_api_key = props.data.has_api_key
  form.base_url = props.data.base_url
  form.model = props.data.model
}

syncFromProps()

watch(() => props.data, () => {
  syncFromProps()
}, { deep: true })

const notifyErrorIfNeeded = (err: unknown, fallback: string) => {
  const known = err as Error & { __notified?: boolean }
  if (known?.__notified) return
  notifyError(known?.message || fallback)
}

const save = async () => {
  submitting.value = true
  try {
    const payload = {
      enabled: form.enabled,
      api_key: form.api_key,
      base_url: form.base_url,
      model: form.model,
    }
    const res = await adminAPI.updateTranslationSettings(payload)
    const data = res.data?.data as Record<string, unknown> | undefined
    form.api_key = ''
    form.has_api_key = !!data?.has_api_key || form.has_api_key
    notifySuccess(t('admin.settings.alerts.saveSuccess'))
    emit('saved')
  } catch (err) {
    notifyErrorIfNeeded(err, t('admin.settings.alerts.saveFailed'))
  } finally {
    submitting.value = false
  }
}

defineExpose({ save, submitting })
</script>

<template>
  <div class="space-y-6">
    <div class="rounded-xl border border-border bg-card">
      <div class="border-b border-border bg-muted/40 px-6 py-4">
        <h2 class="text-lg font-semibold">{{ t('admin.settings.translation.title') }}</h2>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.settings.translation.subtitle') }}</p>
      </div>

      <div class="space-y-6 p-6">
        <div class="flex items-center gap-3 rounded-lg border border-border bg-muted/20 px-4 py-3">
          <Switch id="translation-enabled" v-model="form.enabled" />
          <Label for="translation-enabled" class="text-sm font-medium">{{ t('admin.settings.translation.enabled') }}</Label>
        </div>

        <div class="grid grid-cols-1 gap-6 md:grid-cols-2">
          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.translation.apiKey') }}</label>
            <Input v-model="form.api_key" type="password" :placeholder="t('admin.settings.translation.apiKeyPlaceholder')" />
            <p class="text-xs text-muted-foreground">
              {{ form.has_api_key ? t('admin.settings.translation.apiKeyHintKeep') : t('admin.settings.translation.apiKeyHintEmpty') }}
            </p>
          </div>
          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.translation.baseUrl') }}</label>
            <Input v-model="form.base_url" :placeholder="t('admin.settings.translation.baseUrlPlaceholder')" />
          </div>
          <div class="space-y-2">
            <label class="text-xs font-medium text-muted-foreground">{{ t('admin.settings.translation.model') }}</label>
            <Input v-model="form.model" :placeholder="t('admin.settings.translation.modelPlaceholder')" />
            <p class="text-xs text-muted-foreground">{{ t('admin.settings.translation.modelHint') }}</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
