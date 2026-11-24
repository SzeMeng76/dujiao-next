<template>
  <div class="mx-auto w-full max-w-[1180px] px-6 pb-6">
    <BreadcrumbNav
      class="pb-1 pt-5"
      :items="[
        { label: t('nav.home'), to: '/' },
        { label: pageTitle },
      ]"
    />
    <h1 class="mb-3.5 break-words text-3xl font-extrabold">{{ pageTitle }}</h1>

    <div class="grid items-start gap-7 py-1.5 pb-9 lg:grid-cols-[248px_1fr]">
      <!-- 筛选侧栏：桌面竖排 + 移动端横向 chips -->
      <div class="grid min-w-0 gap-3 lg:sticky lg:top-[88px] lg:gap-5">
        <!-- 搜索框 -->
        <div class="relative">
          <Search class="pointer-events-none absolute left-3.5 top-1/2 h-[18px] w-[18px] -translate-y-1/2 text-muted-foreground" />
          <input
            v-model="searchQuery"
            class="h-11 w-full rounded-xl border border-border bg-card pl-10 pr-10 text-[15px] text-foreground transition-colors placeholder:text-muted-foreground hover:border-hairline-strong"
            :placeholder="t('products.searchBoxPlaceholder')"
            :aria-label="t('products.searchLabel')"
          />
          <button v-if="searchQuery" type="button" class="absolute right-2.5 top-1/2 grid h-6 w-6 -translate-y-1/2 place-items-center rounded-full text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground" :aria-label="t('blog.searchClear')" @click="clearSearch"><X class="h-3.5 w-3.5" /></button>
        </div>

        <!-- 分类：使用 VaultCategorySidebar，移动端自动变横向 chips -->
        <VaultCategorySidebar
          :category-groups="categoryGroups"
          :selected-category="selectedCategory"
          :expanded-parent-ids="expandedParentIds"
          @select="selectCategory"
          @toggle="toggleParentCategory"
        />
      </div>

      <!-- 商品区 -->
      <section>
        <!-- 结果栏：命中数量 + 视图切换 -->
        <div v-if="!loading && products.length" class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <p class="text-[13.5px] font-semibold text-muted-foreground">{{ t('vault.resultsCount', { count: total || products.length }) }}</p>
          <VaultViewToggle v-model="viewMode" />
        </div>

        <div v-if="loading" class="grid gap-4 grid-cols-[repeat(auto-fill,minmax(228px,1fr))]">
          <div v-for="i in 9" :key="i" class="h-[280px] rounded-lg border bg-card"></div>
        </div>

        <template v-else-if="products.length">
          <div v-if="viewMode === 'grid'" class="grid gap-4 grid-cols-[repeat(auto-fill,minmax(228px,1fr))]">
            <VaultProductCard
              v-for="(product, idx) in products"
              :key="product.id"
              :product="product"
              :index="idx"
              @quick-buy="openQuickBuy"
            />
          </div>
          <div v-else class="grid gap-2">
            <VaultProductListItem
              v-for="(product, idx) in products"
              :key="product.id"
              :product="product"
              :index="idx"
              @quick-buy="openQuickBuy"
            />
          </div>

          <PaginationNav
            :current-page="currentPage"
            :total-pages="totalPages"
            :loading="loading"
            compact
            :scroll-top="false"
            @change-page="changePage"
          />
        </template>

        <EmptyState
          v-else
          size="lg"
          :icon="(searchQuery || selectedCategory) ? 'search' : 'package'"
          :title="(searchQuery || selectedCategory) ? t('products.emptyFiltered') : t('products.empty')"
        >
          <template v-if="searchQuery || selectedCategory" #action>
            <Button variant="secondary" class="rounded-full" @click="resetFilters">{{ t('products.clearFilters') }}</Button>
          </template>
        </EmptyState>
      </section>
    </div>

    <ProductQuickBuy
      v-if="quickBuyProduct"
      :product="quickBuyProduct"
      :visible="quickBuyVisible"
      @update:visible="quickBuyVisible = $event"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Search, X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import BreadcrumbNav from '../../components/BreadcrumbNav.vue'
import EmptyState from '../../components/EmptyState.vue'
import PaginationNav from '../../components/PaginationNav.vue'
import { useProductList } from '../../composables/useProductList'
import { usePageSeo } from '../../composables/usePageSeo'
import { useLocalized } from '../../composables/useProduct'
import type { PublicCategory } from '../../utils/category'
import VaultProductCard from './components/VaultProductCard.vue'
import VaultProductListItem from './components/VaultProductListItem.vue'
import VaultViewToggle from './components/VaultViewToggle.vue'
import VaultCategorySidebar from './components/VaultCategorySidebar.vue'
import ProductQuickBuy from '../../components/ProductQuickBuy.vue'

const { t } = useI18n()
const route = useRoute()
const { getLocalizedText } = useLocalized()

const quickBuyProduct = ref<any>(null)
const quickBuyVisible = ref(false)
const openQuickBuy = (product: any) => {
  quickBuyProduct.value = product
  quickBuyVisible.value = true
}

const {
  loading,
  products,
  selectedCategory,
  searchQuery,
  currentPage,
  totalPages,
  total,
  expandedParentIds,
  categoryGroups,
  categoryMap,
  selectCategory,
  toggleParentCategory,
  changePage,
  clearSearch,
  initialize,
  cleanup,
} = useProductList({ pageSize: 12, homeRouteName: 'products' })

// 视图切换只影响本页展示，不写 URL；默认沿用卡片视图
const viewMode = ref<'grid' | 'list'>('grid')

const catName = (cat: PublicCategory) => getLocalizedText(cat.name) || cat.slug || ''

const selectedCategoryName = computed(() => {
  if (!selectedCategory.value) return ''
  const cat = categoryMap.value.get(selectedCategory.value)
  return cat ? catName(cat) : ''
})

const pageTitle = computed(() => {
  if (route.name === 'category-products') return selectedCategoryName.value || t('nav.products')
  return t('nav.products')
})

// 分页窗口：当前页前后各 2 页

const resetFilters = () => {
  clearSearch()
  selectCategory(null)
}

usePageSeo({
  canonicalPath: () => route.path,
  title: () => pageTitle.value,
})

onMounted(() => { void initialize() })
onUnmounted(() => cleanup())
</script>
