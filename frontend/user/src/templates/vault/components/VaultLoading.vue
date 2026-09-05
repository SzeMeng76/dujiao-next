<template>
  <transition name="fade">
    <div v-if="loading"
      class="fixed inset-0 z-50 flex items-center justify-center bg-[var(--bg)]/95 backdrop-blur-xl transition-all duration-500">
      <div class="relative flex flex-col items-center gap-6">
        <!-- Spinner -->
        <div class="relative w-16 h-16">
          <!-- Outer ring -->
          <div
            class="absolute inset-0 rounded-full border-[3px] border-[var(--red-soft)]">
          </div>
          <!-- Spinning ring -->
          <div
            class="absolute inset-0 rounded-full border-[3px] border-transparent border-t-[var(--red)] animate-spin">
          </div>
          <!-- Inner dot -->
          <div
            class="absolute inset-[14px] rounded-full bg-[var(--red)] shadow-[0_0_20px_rgba(79,70,229,0.4)] animate-pulse">
          </div>
        </div>

        <!-- Brand name with dots animation (only show after config loaded) -->
        <div v-if="appStore.config" class="flex items-center gap-1">
          <span class="text-lg font-bold tracking-wide text-[var(--ink)] font-[var(--font-head)]">
            {{ brandName }}
          </span>
          <div class="flex gap-[3px] ml-1">
            <div class="w-1 h-1 bg-[var(--red)] rounded-full animate-bounce [animation-delay:0ms]"></div>
            <div class="w-1 h-1 bg-[var(--red)] rounded-full animate-bounce [animation-delay:150ms]"></div>
            <div class="w-1 h-1 bg-[var(--red)] rounded-full animate-bounce [animation-delay:300ms]"></div>
          </div>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '../../../stores/app'

defineProps<{
  loading: boolean
}>()

const appStore = useAppStore()

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

@keyframes bounce {
  0%, 80%, 100% {
    transform: translateY(0);
  }
  40% {
    transform: translateY(-6px);
  }
}

.animate-bounce {
  animation: bounce 1s infinite;
}

[animation-delay\:0ms] {
  animation-delay: 0ms;
}

[animation-delay\:150ms] {
  animation-delay: 150ms;
}

[animation-delay\:300ms] {
  animation-delay: 300ms;
}
</style>
