<template>
  <div>
    <div class="flex items-center gap-3">
      <button
        type="button"
        class="group relative flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-xl border border-dashed border-input bg-muted/40 transition-colors hover:border-primary/60 hover:bg-muted disabled:cursor-not-allowed"
        :disabled="uploading"
        @click="triggerUpload"
      >
        <img v-if="modelValue" :src="modelValue" alt="" class="h-full w-full object-cover" />
        <Loader2 v-else-if="uploading" class="h-5 w-5 animate-spin text-muted-foreground" />
        <ImagePlus v-else class="h-5 w-5 text-muted-foreground" />
      </button>

      <div class="flex flex-wrap items-center gap-2">
        <Button type="button" variant="outline" size="sm" :disabled="uploading" @click="triggerUpload">
          {{ modelValue ? t('tickets.form.replaceImage') : t('tickets.form.uploadImage') }}
        </Button>
        <Button
          v-if="modelValue"
          type="button"
          variant="ghost"
          size="sm"
          class="text-destructive hover:bg-destructive/10 hover:text-destructive"
          :disabled="uploading"
          @click="emit('update:modelValue', '')"
        >
          {{ t('tickets.form.removeImage') }}
        </Button>
      </div>
    </div>
    <p v-if="error" class="mt-1 text-xs text-destructive">{{ error }}</p>

    <input ref="fileInput" type="file" accept="image/*" class="hidden" @change="onFileChange" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ImagePlus, Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { userTicketAPI } from '../../api'

defineProps<{ modelValue: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const { t } = useI18n()
const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const error = ref('')

const triggerUpload = () => fileInput.value?.click()

const onFileChange = async (e: Event) => {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  uploading.value = true
  error.value = ''
  try {
    const res = await userTicketAPI.uploadImage(file)
    const url = res.data?.data?.url
    if (url) {
      emit('update:modelValue', url)
    }
  } catch (err: any) {
    error.value = err?.response?.data?.msg || t('tickets.uploadFailed')
  } finally {
    uploading.value = false
  }
}
</script>
