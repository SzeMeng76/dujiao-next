<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getImageUrl } from '@/utils/image'

const props = defineProps<{ src?: string }>()

const { t } = useI18n()

// 16/24 是 atelier 模板筛选 chip 与分组标题的尺寸，46 是 vault 首页分类卡，
// 96 用来确认放大后是否发虚。四个尺寸覆盖了现有模板的全部用法。
const SIZES = [16, 24, 46, 96]

const url = computed(() => (props.src ? getImageUrl(props.src) : ''))
const natural = ref<{ w: number; h: number } | null>(null)
const subjectPercent = ref<number | null>(null)
let token = 0

const warnings = computed(() => {
  const size = natural.value
  const list: string[] = []
  if (!size || size.w <= 0 || size.h <= 0) return list
  if (size.w < 512 || size.h < 512) {
    list.push(t('admin.categories.form.iconWarnResolution', { w: size.w, h: size.h }))
  }
  if (size.w !== size.h) {
    list.push(t('admin.categories.form.iconWarnAspect', { w: size.w, h: size.h }))
  }
  if (subjectPercent.value !== null && subjectPercent.value < 70) {
    list.push(t('admin.categories.form.iconWarnPadding', { percent: subjectPercent.value }))
  }
  return list
})

// 量非透明像素的包围盒：AI 生成的图标常留大片透明边，实际显示会比框小一圈
function measureSubject(img: HTMLImageElement) {
  const size = 128
  const canvas = document.createElement('canvas')
  canvas.width = size
  canvas.height = size
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  if (!ctx) return null
  ctx.clearRect(0, 0, size, size)
  ctx.drawImage(img, 0, 0, size, size)

  let data: Uint8ClampedArray
  try {
    data = ctx.getImageData(0, 0, size, size).data
  } catch {
    // 跨域图片会污染画布，此时跳过留白检测
    return null
  }

  let minX = size
  let minY = size
  let maxX = -1
  let maxY = -1
  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      if (data[(y * size + x) * 4 + 3]! > 8) {
        if (x < minX) minX = x
        if (x > maxX) maxX = x
        if (y < minY) minY = y
        if (y > maxY) maxY = y
      }
    }
  }
  if (maxX < 0) return null
  return Math.round((Math.max(maxX - minX + 1, maxY - minY + 1) / size) * 100)
}

async function analyze() {
  const current = ++token
  natural.value = null
  subjectPercent.value = null
  if (!url.value) return

  const img = new Image()
  img.crossOrigin = 'anonymous'
  const loaded = new Promise<boolean>((resolve) => {
    img.onload = () => resolve(true)
    img.onerror = () => resolve(false)
  })
  img.src = url.value
  if (!(await loaded) || current !== token) return

  natural.value = { w: img.naturalWidth, h: img.naturalHeight }
  const percent = measureSubject(img)
  if (current === token) subjectPercent.value = percent
}

watch(url, analyze, { immediate: true })
onBeforeUnmount(() => { token += 1 })
</script>

<template>
  <div v-if="url" class="rounded-lg border border-border bg-muted/40 p-3">
    <p class="mb-2.5 text-xs font-medium text-muted-foreground">
      {{ t('admin.categories.form.iconPreview') }}
    </p>
    <div class="flex flex-wrap items-end gap-4">
      <div v-for="px in SIZES" :key="px" class="flex flex-col items-center gap-1.5">
        <div
          class="grid place-items-center rounded-md bg-white shadow-sm ring-1 ring-black/5"
          :style="{ width: `${px + 16}px`, height: `${px + 16}px` }"
        >
          <img
            :src="url"
            alt=""
            class="object-contain"
            :style="{ width: `${px}px`, height: `${px}px` }"
          />
        </div>
        <span class="text-[10px] tabular-nums text-muted-foreground">{{ px }} px</span>
      </div>
    </div>
    <p class="mt-2.5 text-[11px] leading-relaxed text-muted-foreground">
      {{ t('admin.categories.form.iconPreviewHint') }}
    </p>
    <ul v-if="warnings.length" class="mt-1.5 space-y-1">
      <li
        v-for="warn in warnings"
        :key="warn"
        class="flex gap-1.5 text-[11px] leading-relaxed text-amber-600 dark:text-amber-400"
      >
        <span aria-hidden="true">•</span>
        <span>{{ warn }}</span>
      </li>
    </ul>
  </div>
</template>
