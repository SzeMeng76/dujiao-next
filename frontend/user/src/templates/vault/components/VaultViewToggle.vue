<template>
  <div class="inline-flex items-center gap-0.5 rounded-full border bg-card p-1" role="group" :aria-label="t('vault.viewToggleLabel')">
    <button
      type="button"
      class="inline-flex h-8 items-center gap-1.5 rounded-full px-2.5 text-[12.5px] font-semibold transition-colors"
      :class="modelValue === 'grid' ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary hover:text-foreground'"
      :aria-pressed="modelValue === 'grid'"
      @click="emit('update:modelValue', 'grid')"
    >
      <LayoutGrid class="h-4 w-4" aria-hidden="true" />
      <span class="hidden sm:inline">{{ t('vault.viewGrid') }}</span>
    </button>
    <button
      type="button"
      class="inline-flex h-8 items-center gap-1.5 rounded-full px-2.5 text-[12.5px] font-semibold transition-colors"
      :class="modelValue === 'list' ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-secondary hover:text-foreground'"
      :aria-pressed="modelValue === 'list'"
      @click="emit('update:modelValue', 'list')"
    >
      <List class="h-4 w-4" aria-hidden="true" />
      <span class="hidden sm:inline">{{ t('vault.viewList') }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { LayoutGrid, List } from 'lucide-vue-next'

export type ProductViewMode = 'grid' | 'list'

// 窄屏只留图标，把文字让给标题
defineProps<{ modelValue: ProductViewMode }>()
const emit = defineEmits<{ 'update:modelValue': [value: ProductViewMode] }>()

const { t } = useI18n()
</script>
