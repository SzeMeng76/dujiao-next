<template>
  <Popover v-model:open="open">
    <PopoverTrigger as-child>
      <button
        ref="triggerRef"
        type="button"
        class="flex min-h-11 w-full items-center justify-between gap-2 rounded-xl border border-input bg-transparent px-3 py-2 text-start text-sm transition-colors hover:border-primary/40 focus:outline-none focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
        :disabled="disabled"
      >
        <span v-if="selectedOption" class="min-w-0 flex-1">
          <slot name="selected" :option="selectedOption">
            <span class="block truncate">{{ selectedOption.label }}</span>
          </slot>
        </span>
        <span v-else class="min-w-0 flex-1 truncate text-muted-foreground">{{ placeholder }}</span>

        <span class="flex shrink-0 items-center gap-1">
          <Loader2 v-if="loading" class="h-4 w-4 animate-spin text-muted-foreground" />
          <X
            v-else-if="clearable && selectedOption && !disabled"
            class="h-4 w-4 text-muted-foreground hover:text-foreground"
            @click.stop="clearSelection"
          />
          <ChevronDown v-else class="h-4 w-4 opacity-50" />
        </span>
      </button>
    </PopoverTrigger>

    <PopoverContent align="start" :side-offset="4" class="w-80 min-w-72 p-0">
      <div class="border-b p-2">
        <div ref="searchBox" class="relative">
          <Search class="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            v-model="keyword"
            :placeholder="searchPlaceholder"
            class="h-9 pl-8"
            @keydown.stop
          />
        </div>
      </div>

      <div class="max-h-72 overflow-y-auto p-1">
        <div v-if="loading" class="px-3 py-6 text-center text-sm text-muted-foreground">
          {{ loadingText }}
        </div>
        <div v-else-if="errorText" class="px-3 py-6 text-center text-sm text-destructive">
          {{ errorText }}
        </div>
        <div v-else-if="options.length === 0" class="px-3 py-6 text-center text-sm text-muted-foreground">
          {{ emptyText }}
        </div>
        <button
          v-for="option in options"
          v-else
          :key="String(option.value)"
          type="button"
          class="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-start transition-colors hover:bg-muted"
          :class="isSelected(option) ? 'bg-primary/10' : ''"
          @click="selectOption(option)"
        >
          <slot name="option" :option="option">
            <span class="min-w-0 flex-1 truncate text-sm">{{ option.label }}</span>
          </slot>
        </button>
      </div>
    </PopoverContent>
  </Popover>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ChevronDown, Loader2, Search, X } from 'lucide-vue-next'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { debounceAsync } from '../utils/debounce'

export interface SearchSelectOption {
  value: string | number
  label: string
  description?: string
  image?: string
  [key: string]: unknown
}

const props = withDefaults(
  defineProps<{
    modelValue: string | number | null | undefined
    options: SearchSelectOption[]
    loading?: boolean
    disabled?: boolean
    clearable?: boolean
    placeholder?: string
    searchPlaceholder?: string
    emptyText?: string
    loadingText?: string
    errorText?: string
    /** 输入后多久发起远程搜索，避免每次按键都请求 */
    searchDelay?: number
  }>(),
  {
    loading: false,
    disabled: false,
    clearable: true,
    placeholder: '',
    searchPlaceholder: '',
    emptyText: '',
    loadingText: '',
    errorText: '',
    searchDelay: 300,
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string | number | null): void
  (e: 'search', keyword: string): void
  (e: 'select', option: SearchSelectOption | null): void
}>()

const open = ref(false)
const keyword = ref('')
const searchBox = ref<HTMLElement | null>(null)

const selectedOption = computed(() =>
  props.options.find((option) => String(option.value) === String(props.modelValue ?? '')) ?? null,
)

const isSelected = (option: SearchSelectOption) => String(option.value) === String(props.modelValue ?? '')

const runSearch = debounceAsync(async (value: string) => {
  emit('search', value)
}, props.searchDelay)

// 打开面板时聚焦搜索框，关闭时重置关键词并回到完整列表
watch(open, async (isOpen) => {
  if (isOpen) {
    keyword.value = ''
    emit('search', '')
    await nextTick()
    searchBox.value?.querySelector('input')?.focus()
    return
  }
  runSearch.cancel()
})

watch(keyword, (value) => {
  runSearch(String(value ?? '').trim())
})

const selectOption = (option: SearchSelectOption) => {
  emit('update:modelValue', option.value)
  emit('select', option)
  open.value = false
}

const clearSelection = () => {
  emit('update:modelValue', null)
  emit('select', null)
}
</script>
