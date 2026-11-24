<template>
  <div>
    <!-- 公告跑马灯（后台公告展示方式选“跑马灯”时显示，两种模式共用） -->
    <div v-if="tickerAnnouncement" class="mx-auto w-full max-w-[1180px] px-4 pt-5 sm:px-6">
      <AnnouncementTicker :announcement="tickerAnnouncement" @close="closeTicker" />
    </div>

    <!-- ==================== 列表模式 ==================== -->
    <template v-if="isListMode">
      <!-- 站长配置的横幅轮播 -->
      <VaultBannerHero />
      <section class="mx-auto w-full max-w-[1180px] px-4 py-6 sm:px-6">
        <div class="grid items-start gap-5 lg:grid-cols-[248px_1fr] lg:gap-7">
          <VaultCategorySidebar
            :category-groups="categoryGroups"
            :selected-category="selectedCategory"
            :expanded-parent-ids="expandedParentIds"
            @select="selectCategory"
            @toggle="toggleParentCategory"
          />

          <main class="min-w-0">
            <!-- 搜索 -->
            <div class="relative mb-4">
              <Search class="pointer-events-none absolute left-3.5 top-1/2 h-[18px] w-[18px] -translate-y-1/2 text-muted-foreground" />
              <input
                v-model="searchQuery"
                class="h-11 w-full rounded-xl border border-border bg-card pl-10 pr-10 text-[15px] text-foreground transition-colors placeholder:text-muted-foreground hover:border-hairline-strong"
                :placeholder="t('products.searchBoxPlaceholder')"
                :aria-label="t('products.searchLabel')"
              />
              <button v-if="searchQuery" type="button" class="absolute right-2.5 top-1/2 grid h-6 w-6 -translate-y-1/2 place-items-center rounded-full text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground" :aria-label="t('blog.searchClear')" @click="clearSearch"><X class="h-3.5 w-3.5" /></button>
            </div>

            <!-- 骨架 -->
            <div v-if="listLoading" class="space-y-2.5">
              <div v-for="i in 8" :key="i" class="h-[76px] rounded-lg border bg-card"></div>
            </div>

            <!-- 分组列表 -->
            <div v-else-if="listProductGroups.length" class="space-y-6">
              <div v-for="group in listProductGroups" :key="group.categoryId ?? 'uncategorized'">
                <div class="mb-2.5 flex items-center gap-2 px-0.5">
                  <span class="h-4 w-1 flex-none rounded-full bg-primary"></span>
                  <img v-if="group.categoryIcon" :src="getImageUrl(group.categoryIcon)" :alt="group.categoryName" loading="lazy" class="h-5 w-5 flex-none rounded object-cover" />
                  <span class="min-w-0 truncate text-sm font-bold text-foreground">{{ group.categoryName }}</span>
                  <span class="flex-none text-xs text-muted-foreground">({{ group.products.length }})</span>
                </div>
                <div class="space-y-2">
                  <VaultProductListItem
                    v-for="(product, idx) in group.products"
                    :key="product.id"
                    :product="product"
                    :index="idx"
                    @quick-buy="openQuickBuy"
                  />
                </div>
              </div>

              <PaginationNav
                :current-page="listCurrentPage"
                :total-pages="listTotalPages"
                :loading="listLoading"
                compact
                :scroll-top="false"
                @change-page="listChangePage"
              />
            </div>

            <!-- 空态 -->
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
          </main>
        </div>
      </section>
    </template>

    <!-- ==================== 卡片模式（默认） ==================== -->
    <template v-else>
      <!-- 搜索中枢：输入即过滤，命中时下方落地页内容整体换成结果网格 -->
      <section class="mx-auto w-full max-w-[1180px] px-4 pt-6 sm:px-6">
        <form
          class="mx-auto flex max-w-[640px] items-center gap-2 rounded-lg border bg-card p-1.5 shadow-[var(--shadow)] transition-colors focus-within:border-hairline-strong"
          role="search"
          @submit.prevent="onHomeSearchSubmit"
        >
          <Search class="ml-2.5 h-[18px] w-[18px] shrink-0 text-muted-foreground" aria-hidden="true" />
          <input
            v-model="searchQuery"
            type="search"
            enterkeyhint="search"
            class="vault-home-search h-10 min-w-0 flex-1 border-none bg-transparent px-1 text-[15px] text-foreground outline-none placeholder:text-muted-foreground"
            :placeholder="t('products.searchBoxPlaceholder')"
            :aria-label="t('products.searchLabel')"
          />
          <button
            v-if="searchQuery"
            type="button"
            class="mr-1 grid h-8 w-8 shrink-0 place-items-center rounded-full text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
            :aria-label="t('blog.searchClear')"
            @click="searchQuery = ''"
          >
            <X class="h-4 w-4" />
          </button>
        </form>
      </section>

      <!-- 搜索结果：复用列表模式的 useProductList 实例，原地渲染 -->
      <section v-if="homeSearchActive" class="mx-auto w-full max-w-[1180px] px-4 py-7 sm:px-6">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <p class="min-w-0 truncate text-[13.5px] font-semibold text-muted-foreground">
            {{ t('vault.resultsCount', { count: listTotal || listProducts.length }) }}
          </p>
          <div class="flex items-center gap-2">
            <VaultViewToggle v-model="viewMode" />
            <Button variant="ghost" size="sm" class="rounded-full" @click="searchQuery = ''">
              {{ t('products.clearFilters') }}
            </Button>
          </div>
        </div>

        <div v-if="listLoading" class="grid gap-4 grid-cols-[repeat(auto-fill,minmax(228px,1fr))]">
          <div v-for="i in 9" :key="i" class="h-[280px] rounded-lg border bg-card"></div>
        </div>
        <template v-else-if="listProducts.length">
          <div v-if="viewMode === 'grid'" class="grid gap-4 grid-cols-[repeat(auto-fill,minmax(228px,1fr))]">
            <VaultProductCard
              v-for="(product, idx) in listProducts"
              :key="product.id"
              :product="product"
              :index="idx"
              @quick-buy="openQuickBuy"
            />
          </div>
          <div v-else class="grid gap-2">
            <VaultProductListItem
              v-for="(product, idx) in listProducts"
              :key="product.id"
              :product="product"
              :index="idx"
              @quick-buy="openQuickBuy"
            />
          </div>
        </template>
        <EmptyState v-else size="lg" icon="search" :title="t('products.emptyFiltered')">
          <template #action>
            <Button variant="secondary" class="rounded-full" @click="searchQuery = ''">{{ t('products.clearFilters') }}</Button>
          </template>
        </EmptyState>

        <PaginationNav
          :current-page="listCurrentPage"
          :total-pages="listTotalPages"
          :loading="listLoading"
          compact
          :scroll-top="false"
          @change-page="listChangePage"
        />
      </section>

      <!-- 落地页内容：有搜索词时整体让位给结果 -->
      <template v-else>
        <!-- 横幅轮播 + 最新动态：大屏并排，窄屏上下堆叠 -->
        <VaultBannerHero :with-aside="latestPanelVisible">
          <template #aside>
            <aside class="flex h-full flex-col rounded-lg border bg-card p-[18px] shadow-[var(--shadow)]">
              <div class="flex items-center justify-between gap-3 border-b pb-3">
                <h2 class="text-[15px] font-extrabold">{{ t('home.latest.title') }}</h2>
                <div class="flex items-center gap-3 text-[12.5px] font-bold text-muted-foreground">
                  <RouterLink v-if="blogEnabled" to="/blog" class="transition-colors hover:text-primary">{{ t('nav.blog') }}</RouterLink>
                  <RouterLink v-if="noticeEnabled" to="/notice" class="transition-colors hover:text-primary">{{ t('nav.notice') }}</RouterLink>
                </div>
              </div>
              <ol class="mt-1 flex-1 divide-y">
                <li v-for="post in posts" :key="post.id">
                  <RouterLink
                    :to="`/blog/${post.slug}`"
                    class="flex items-center gap-2.5 py-2.5 text-[13px] text-foreground transition-colors hover:text-primary"
                  >
                    <span class="shrink-0 rounded-full bg-secondary px-2 py-0.5 text-[10.5px] font-bold text-muted-foreground">
                      {{ postTypeLabel(post) }}
                    </span>
                    <span class="min-w-0 flex-1 truncate font-semibold">{{ getLocalizedText(post.title) }}</span>
                    <time class="shrink-0 text-[11px] font-semibold text-muted-foreground">{{ formatShortDate(post.published_at) }}</time>
                  </RouterLink>
                </li>
              </ol>
            </aside>
          </template>
        </VaultBannerHero>

        <!-- 分类块 -->
        <section v-if="topCategories.length" class="mx-auto w-full max-w-[1180px] px-4 py-9 sm:px-6">
          <div class="mb-[22px] flex flex-wrap items-end justify-between gap-4">
            <h2 class="text-[28px] font-extrabold">{{ t('vault.categoriesTitle') }}</h2>
            <Button as-child variant="ghost" size="sm" class="rounded-full"><RouterLink to="/products">{{ t('vault.allCategories') }} <ChevronRight /></RouterLink></Button>
          </div>
          <div class="grid gap-3.5 grid-cols-[repeat(auto-fill,minmax(150px,1fr))]">
            <RouterLink
              v-for="(cat, idx) in topCategories"
              :key="cat.id"
              class="relative flex min-h-[116px] flex-col justify-between gap-2.5 overflow-hidden rounded-lg p-[18px] transition hover:-translate-y-[3px] hover:shadow-[var(--shadow)]"
              :class="catColor(idx)"
              :to="`/categories/${cat.slug}`"
            >
              <span class="grid h-10 w-10 place-items-center rounded-xl bg-white/20">
                <img v-if="cat.icon" :src="getImageUrl(cat.icon)" :alt="catName(cat)" loading="lazy" class="h-6 w-6 object-contain" />
                <Tag v-else class="h-[22px] w-[22px]" />
              </span>
              <div class="min-w-0"><div class="line-clamp-2 break-words font-bold">{{ catName(cat) }}</div></div>
            </RouterLink>
          </div>
        </section>

        <!-- 热门商品 -->
        <section class="mx-auto w-full max-w-[1180px] px-4 py-9 sm:px-6">
          <div class="mb-[22px] flex flex-wrap items-end justify-between gap-4">
            <h2 class="text-[28px] font-extrabold">{{ t('home.featured.title') }}</h2>
            <div class="flex items-center gap-2">
              <VaultViewToggle v-model="viewMode" />
              <Button as-child variant="outline" size="sm" class="rounded-full"><RouterLink to="/products">{{ t('home.featured.viewAll') }}</RouterLink></Button>
            </div>
          </div>

          <div v-if="productsLoading" class="grid gap-4 grid-cols-[repeat(auto-fill,minmax(228px,1fr))]">
            <div v-for="i in 10" :key="i" class="h-[280px] rounded-lg border bg-card"></div>
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
          </template>
          <div v-else class="flex flex-col items-center gap-3 rounded-lg border border-dashed py-14 text-center text-muted-foreground">
            <PackageOpen class="h-12 w-12 opacity-60" />
            <p>{{ t('home.featured.empty') }}</p>
          </div>
        </section>
      </template>
    </template>

    <ProductQuickBuy
      v-if="quickBuyProduct"
      :product="quickBuyProduct"
      :visible="quickBuyVisible"
      @update:visible="quickBuyVisible = $event"
    />

    <AnnouncementModal
      v-if="activeAnnouncement"
      :announcement="activeAnnouncement"
      :visible="announcementVisible"
      @update:visible="announcementVisible = $event"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ChevronRight, PackageOpen, Search, Tag, X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import EmptyState from '../../components/EmptyState.vue'
import PaginationNav from '../../components/PaginationNav.vue'
import { categoryAPI, postAPI, productAPI } from '../../api'
import { buildCategoryGroups, type PublicCategory } from '../../utils/category'
import { getImageUrl } from '../../utils/image'
import { useLocalized } from '../../composables/useProduct'
import { useProductList } from '../../composables/useProductList'
import { useProductListGroups } from '../../composables/useProductListGroups'
import { usePageSeo } from '../../composables/usePageSeo'
import { useAppStore } from '../../stores/app'
import VaultProductCard from './components/VaultProductCard.vue'
import VaultProductListItem from './components/VaultProductListItem.vue'
import VaultViewToggle from './components/VaultViewToggle.vue'
import VaultCategorySidebar from './components/VaultCategorySidebar.vue'
import VaultBannerHero from './components/VaultBannerHero.vue'
import ProductQuickBuy from '../../components/ProductQuickBuy.vue'
import AnnouncementModal from '../../components/AnnouncementModal.vue'
import AnnouncementTicker from '../../components/AnnouncementTicker.vue'
import { useAnnouncement, type HomeAnnouncement } from '../../composables/useAnnouncement'

const route = useRoute()
const { t } = useI18n()
const { getLocalizedText } = useLocalized()
const appStore = useAppStore()

const isListMode = computed(() => appStore.config?.template_mode === 'list')

// 卡片模式：热门商品与搜索结果共用同一个视图偏好，切换后两处保持一致
const viewMode = ref<'grid' | 'list'>('grid')

// ==================== 快速购买 ====================
const quickBuyProduct = ref<any>(null)
const quickBuyVisible = ref(false)
const openQuickBuy = (product: any) => {
  quickBuyProduct.value = product
  quickBuyVisible.value = true
}

// ==================== 列表模式 ====================
const {
  loading: listLoading,
  products: listProducts,
  selectedCategory,
  searchQuery,
  currentPage: listCurrentPage,
  totalPages: listTotalPages,
  total: listTotal,
  expandedParentIds,
  categoryGroups,
  categoryMap: listCategoryMap,
  selectCategory,
  toggleParentCategory,
  changePage: listChangePage,
  clearSearch,
  initialize: listInitialize,
  cleanup: listCleanup,
} = useProductList({
  pageSize: 20,
  homeRouteName: 'home',
  // 首页未筛选时用分类精选（每类固定 N 个），避免只取列表第一页导致分类缺席
  showcase: { perCategory: 4, maxCategories: 12 },
})

const listProductGroups = useProductListGroups(listProducts, listCategoryMap)

// ==================== 卡片模式：首页搜索 ====================
// 复用列表模式的 useProductList 实例原地过滤，关键词非空时整段落地页让位给结果网格
const homeSearchActive = computed(() => searchQuery.value.trim() !== '')

// 卡片模式下 useProductList 未初始化（initializing 为 true 会吞掉输入监听），首次搜索时补一次
let homeListReady = false
const ensureHomeListReady = async () => {
  if (homeListReady || isListMode.value) return
  homeListReady = true
  await listInitialize()
}

// 结果随输入即时更新，回车只需收起移动端软键盘——没有独立的「搜索」按钮可点
const onHomeSearchSubmit = () => {
  if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
}

watch(
  () => searchQuery.value.trim(),
  (keyword) => {
    if (isListMode.value || !keyword) return
    void ensureHomeListReady()
  },
)

const resetFilters = () => {
  clearSearch()
  selectCategory(null)
}

// ==================== 卡片模式 ====================
const products = ref<any[]>([])
const productsLoading = ref(true)
const posts = ref<any[]>([])
const topCategories = ref<PublicCategory[]>([])

const catColors = [
  'bg-[color:var(--red)] text-white',
  'bg-[color:var(--teal)] text-white',
  'bg-[color:var(--plum)] text-white',
  'bg-[color:var(--gold)] text-[color:var(--on-gold)]',
  'bg-[color:var(--ink)] text-[color:var(--bg)]',
]
const catColor = (idx: number) => catColors[idx % catColors.length]
const catName = (cat: PublicCategory) => getLocalizedText(cat.name) || cat.slug || ''

const navBuiltin = computed(() => (appStore.config?.nav_config as { builtin?: Record<string, boolean> } | undefined)?.builtin)
const blogEnabled = computed(() => navBuiltin.value?.blog !== false)
const noticeEnabled = computed(() => navBuiltin.value?.notice !== false)
const latestVisible = computed(() => blogEnabled.value || noticeEnabled.value)
// 有动态且已拉到内容时才在横幅右侧插卡，避免出现空白栏
const latestPanelVisible = computed(() => latestVisible.value && posts.value.length > 0)

const postTypeLabel = (post: { type?: string }) => (post.type === 'notice' ? t('nav.notice') : t('nav.blog'))

const formatShortDate = (value: string) => {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return ''
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

// ==================== 公告弹窗（版本化可关闭，两种模式共用） ====================
const { shouldShow, shouldShowTicker, closeTickerForSession } = useAnnouncement()
const activeAnnouncement = ref<HomeAnnouncement | null>(null)
const announcementVisible = ref(false)

// 跑马灯：公告展示方式为 ticker 时显示，且不再弹窗
const tickerClosed = ref(false)
const tickerAnnouncement = computed(() => {
  if (tickerClosed.value) return null
  const announcement = appStore.config?.announcement as HomeAnnouncement | undefined
  return announcement && shouldShowTicker(announcement) ? announcement : null
})
const closeTicker = () => {
  if (tickerAnnouncement.value) closeTickerForSession(tickerAnnouncement.value.version)
  tickerClosed.value = true
}

const showAnnouncementIfNeeded = () => {
  const announcement = appStore.config?.announcement as HomeAnnouncement | undefined
  if (announcement && announcement.display !== 'ticker' && shouldShow(announcement)) {
    activeAnnouncement.value = announcement
    announcementVisible.value = true
  }
}

const loadProducts = async () => {
  productsLoading.value = true
  try {
    const res = await productAPI.list({ page: 1, page_size: 15 })
    products.value = res.data.data || []
  } catch (err) {
    console.error('Failed to load products:', err)
  } finally {
    productsLoading.value = false
  }
}

const loadCategories = async () => {
  try {
    const res = await categoryAPI.list()
    const limit = Number(appStore.config?.home_category_limit) || 10
    topCategories.value = buildCategoryGroups(res.data.data || []).slice(0, limit)
  } catch (err) {
    console.error('Failed to load categories:', err)
  }
}

const loadPosts = async () => {
  if (!latestVisible.value) return
  try {
    const params: Record<string, unknown> = { page: 1, page_size: 4 }
    if (blogEnabled.value && !noticeEnabled.value) params.type = 'blog'
    if (!blogEnabled.value && noticeEnabled.value) params.type = 'notice'
    const res = await postAPI.list(params)
    posts.value = res.data.data || []
  } catch (err) {
    console.error('Failed to load posts:', err)
  }
}

// ==================== SEO ====================
const seoCategoryName = computed(() => {
  if (!selectedCategory.value) return ''
  const cat = listCategoryMap.value.get(selectedCategory.value)
  return cat ? catName(cat) : ''
})
usePageSeo({
  canonicalPath: () => route.path,
  title: () => {
    if (route.name === 'category-products') return seoCategoryName.value || t('nav.products')
    if (route.name === 'products') return t('nav.products')
    return undefined
  },
})

onMounted(async () => {
  await appStore.loadConfig()
  if (isListMode.value) {
    await listInitialize()
  } else {
    await Promise.all([loadProducts(), loadCategories(), loadPosts()])
    // 带 ?q= 进来（刷新 / 分享 / 前进后退）时直接落到搜索结果，避免看到空输入框的落地页
    const keyword = (Array.isArray(route.query.q) ? route.query.q[0] : route.query.q) ?? ''
    if (String(keyword).trim()) {
      searchQuery.value = String(keyword).trim()
      await ensureHomeListReady()
    }
  }
  showAnnouncementIfNeeded()
})

onUnmounted(() => listCleanup())
</script>

<style scoped>
/* 收起 WebKit 自带的搜索清除按钮，改用自定义的 X */
.vault-home-search::-webkit-search-cancel-button {
  display: none;
}
</style>
