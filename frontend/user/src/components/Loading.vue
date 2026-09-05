<template>
  <transition name="fade">
    <div v-if="loading" class="fixed inset-0 z-50 flex items-center justify-center backdrop-blur-xl transition-all duration-500"
      :class="isVault ? 'bg-[var(--bg)]/95' : 'theme-overlay'">

      <!-- Vault 样式 -->
      <div v-if="isVault" class="relative flex flex-col items-center gap-6">
        <!-- Spinner -->
        <div class="relative w-16 h-16">
          <!-- Outer ring -->
          <div class="absolute inset-0 rounded-full border-[3px] border-[var(--red-soft)]"></div>
          <!-- Spinning ring -->
          <div class="absolute inset-0 rounded-full border-[3px] border-transparent border-t-[var(--red)] animate-spin"></div>
          <!-- Inner dot -->
          <div class="absolute inset-[14px] rounded-full bg-[var(--red)] shadow-[0_0_20px_rgba(79,70,229,0.4)] animate-pulse"></div>
        </div>

        <!-- Brand name with dots animation (only show after config loaded) -->
        <div v-if="appStore.config" class="flex items-center gap-1">
          <span class="text-lg font-bold tracking-wide text-[var(--ink)] font-[var(--font-head)]">
            {{ brandName }}
          </span>
          <div class="flex gap-[3px] ml-1">
            <div class="w-1 h-1 bg-[var(--red)] rounded-full animate-bounce delay-0"></div>
            <div class="w-1 h-1 bg-[var(--red)] rounded-full animate-bounce delay-150"></div>
            <div class="w-1 h-1 bg-[var(--red)] rounded-full animate-bounce delay-300"></div>
          </div>
        </div>
      </div>

      <!-- Classic 样式 -->
      <div v-else class="relative flex flex-col items-center gap-8">
        <div class="relative w-20 h-20">
          <div class="absolute inset-0 rounded-full border"></div>
          <div
            class="absolute inset-0 rounded-full border-2 border-transparent border-t-current animate-spin text-foreground">
          </div>
          <div
            class="absolute inset-[10px] rounded-full bg-secondary border flex items-center justify-center">
            <div
              class="w-2 h-2 rounded-full bg-current text-muted-foreground">
            </div>
          </div>
          <div
            class="absolute inset-[-8px] rounded-full border border-transparent border-b-gray-300 dark:border-b-white/20 animate-spin-reverse">
          </div>
        </div>

        <!-- Text -->
        <div class="flex flex-col items-center gap-2">
          <div class="text-xl font-bold tracking-[0.2em] text-foreground animate-pulse">
            LOADING
          </div>
          <div class="flex gap-1 h-1">
            <div class="w-1 h-1 bg-current text-muted-foreground rounded-full animate-bounce delay-100"></div>
            <div class="w-1 h-1 bg-current text-muted-foreground rounded-full animate-bounce delay-200"></div>
            <div class="w-1 h-1 bg-current text-muted-foreground rounded-full animate-bounce delay-300"></div>
          </div>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '../stores/app'
import { getActiveTemplate } from '../templates/registry'

defineProps<{
  loading: boolean
}>()

const appStore = useAppStore()
const isVault = computed(() => getActiveTemplate() === 'vault')

const brandName = computed(() => {
  const text = String(appStore.config?.brand?.site_name || '').trim()
  return text !== '' ? text : 'D&J Studio'
})
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

.animate-spin-reverse {
  animation: spin-reverse 3s linear infinite;
}

@keyframes spin-reverse {
  from {
    transform: rotate(360deg);
  }

  to {
    transform: rotate(0deg);
  }
}

.delay-0 {
  animation-delay: 0ms;
}

.delay-100 {
  animation-delay: 0.1s;
}

.delay-150 {
  animation-delay: 150ms;
}

.delay-200 {
  animation-delay: 0.2s;
}

.delay-300 {
  animation-delay: 0.3s;
}
</style>
