<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { Sparkles, Loader2 } from 'lucide-vue-next'

defineProps<{ loading: boolean; disabled?: boolean }>()
const emit = defineEmits<{ click: [] }>()
const { t } = useI18n()
</script>

<template>
  <Button
    type="button"
    size="sm"
    :disabled="loading || disabled"
    class="gap-1.5 border-0 bg-gradient-to-r from-violet-600 to-indigo-600 text-white shadow-sm transition-opacity hover:from-violet-600 hover:to-indigo-600 hover:opacity-90 disabled:opacity-60"
    @click="emit('click')"
  >
    <Loader2 v-if="loading" class="h-3.5 w-3.5 animate-spin" />
    <Sparkles v-else class="h-3.5 w-3.5" />
    {{ loading ? t('common.translate.translating') : t('common.translate.button') }}
  </Button>
</template>
