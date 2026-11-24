<template>
  <section v-if="showHeroSection || withAside" class="mx-auto w-full max-w-[1180px] px-4 pt-6 sm:px-6">
    <div class="grid gap-4 lg:grid-cols-12">
      <div
        v-if="showHeroSection"
        class="relative h-full overflow-hidden rounded-lg border bg-card shadow-[var(--shadow)]"
        :class="withAside ? 'lg:col-span-7' : 'lg:col-span-12'"
        @touchstart="onBannerTouchStart"
        @touchend="onBannerTouchEnd"
      >
      <Transition name="vault-banner-fade" mode="out-in">
        <img
          v-if="!bannerLoading && heroImage"
          :src="heroImage"
          :key="heroImage"
          :alt="heroTitle"
          class="absolute inset-0 h-full w-full object-cover"
        />
      </Transition>
      <div class="absolute inset-0 bg-gradient-to-r from-black/65 via-black/40 to-black/15"></div>

      <!-- Loading skeleton -->
      <div v-if="bannerLoading" class="relative flex min-h-[180px] flex-col justify-end gap-3 p-5 sm:min-h-[240px] sm:p-7 md:min-h-[300px] md:p-9">
        <div class="h-6 w-24 animate-pulse rounded-full bg-white/30"></div>
        <div class="h-8 w-3/4 max-w-[520px] animate-pulse rounded-lg bg-white/30"></div>
        <div class="h-4 w-1/2 max-w-[360px] animate-pulse rounded bg-white/25"></div>
      </div>

      <!-- Content -->
      <div v-else class="relative flex min-h-[180px] flex-col justify-between gap-4 p-5 sm:min-h-[240px] sm:p-7 md:min-h-[300px] md:p-9">
        <div v-if="bannerCount > 1" class="flex items-center justify-end gap-2">
          <button
            type="button"
            class="grid h-9 w-9 place-items-center rounded-full border border-white/30 bg-black/25 text-white transition hover:bg-black/45"
            :aria-label="t('common.previousBanner')"
            @click="handlePrevHeroBanner"
          >
            <ChevronLeft class="h-4 w-4" />
          </button>
          <button
            type="button"
            class="grid h-9 w-9 place-items-center rounded-full border border-white/30 bg-black/25 text-white transition hover:bg-black/45"
            :aria-label="t('common.nextBanner')"
            @click="handleNextHeroBanner"
          >
            <ChevronRight class="h-4 w-4" />
          </button>
        </div>

        <div class="mt-auto max-w-[640px] space-y-2.5">
          <span class="inline-flex items-center gap-1.5 rounded-full border border-white/25 bg-black/45 px-3 py-1 text-[12px] font-bold uppercase tracking-[0.04em] text-white backdrop-blur-sm">
            <Zap class="h-3.5 w-3.5" /> {{ heroBadge }}
          </span>
          <h2 class="text-xl font-extrabold leading-tight text-white sm:text-2xl md:text-[2rem]">{{ heroTitle }}</h2>
          <p class="line-clamp-2 max-w-[52ch] text-sm leading-relaxed text-white/85 sm:text-base">{{ heroSubtitle }}</p>

          <div class="flex flex-wrap items-center gap-3 pt-1.5">
            <button
              type="button"
              class="inline-flex min-h-[40px] items-center gap-2 rounded-full bg-white px-5 py-2.5 text-sm font-bold text-[color:var(--ink)] transition hover:scale-[1.03]"
              @click="goToHeroLink"
            >
              {{ heroPrimaryButtonText }}
              <ArrowRight class="h-4 w-4" />
            </button>
          </div>
        </div>

        <div v-if="bannerCount > 1" class="flex items-center gap-2">
          <button
            v-for="(_, idx) in banners"
            :key="`vault-banner-dot-${idx}`"
            type="button"
            class="h-2 rounded-full transition-all"
            :class="idx === currentBannerIndex ? 'w-6 bg-white' : 'w-2 bg-white/45 hover:bg-white/70'"
            :aria-label="t('common.switchBanner', { n: idx + 1 })"
            @click="selectHeroBanner(idx)"
          ></button>
        </div>
      </div>
      </div>

      <aside
        v-if="withAside"
        :class="showHeroSection ? 'lg:col-span-5' : 'lg:col-span-12'"
      >
        <slot name="aside" />
      </aside>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowRight, ChevronLeft, ChevronRight, Zap } from 'lucide-vue-next'
import { useBannerCarousel } from '../../../composables/useBannerCarousel'

const { t } = useI18n()

// 传入 #aside 插槽时，横幅收窄到 12 栏网格的左 7 栏，右侧 5 栏留给外部面板
withDefaults(defineProps<{ withAside?: boolean }>(), { withAside: false })

const {
  banners,
  bannerLoading,
  currentBannerIndex,
  bannerCount,
  showHeroSection,
  heroImage,
  heroBadge,
  heroTitle,
  heroSubtitle,
  heroPrimaryButtonText,
  loadBanners,
  handleNextHeroBanner,
  handlePrevHeroBanner,
  selectHeroBanner,
  goToHeroLink,
  onBannerTouchStart,
  onBannerTouchEnd,
  stopHeroAutoPlay,
} = useBannerCarousel()

onMounted(() => { void loadBanners() })
onUnmounted(() => stopHeroAutoPlay())
</script>

<style scoped>
.vault-banner-fade-enter-active,
.vault-banner-fade-leave-active {
  transition: opacity 300ms ease;
}
.vault-banner-fade-enter-from,
.vault-banner-fade-leave-to {
  opacity: 0;
}
</style>
