<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'

defineProps<{ loading: boolean; disabled?: boolean }>()
const emit = defineEmits<{ click: [] }>()
const { t } = useI18n()
</script>

<template>
  <Button
    type="button"
    variant="outline"
    size="sm"
    :disabled="loading || disabled"
    @click="emit('click')"
  >
    <span v-if="loading" class="mr-1 h-3 w-3 animate-spin rounded-full border-2 border-primary/30 border-t-primary"></span>
    {{ loading ? t('admin.common.translate.translating') : t('admin.common.translate.button') }}
  </Button>
</template>
