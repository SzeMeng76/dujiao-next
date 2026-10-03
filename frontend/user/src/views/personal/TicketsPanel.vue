<template>
  <div class="space-y-4">
    <PanelHeading :title="t('tickets.title')" :description="t('tickets.subtitle')" :icon="MessageSquare">
      <template #actions>
        <Button v-if="!showCreate && !activeTicket" type="button" size="sm" class="rounded-full" @click="openCreate">
          {{ t('tickets.create') }}
        </Button>
        <Button v-else type="button" variant="ghost" size="sm" class="rounded-full" @click="backToList">
          {{ t('tickets.backToList') }}
        </Button>
      </template>
    </PanelHeading>

    <Alert v-if="alert" :variant="pageAlertVariant(alert.level)" :class="pageAlertToneClass(alert.level)">
      <AlertDescription>{{ alert.message }}</AlertDescription>
    </Alert>

    <!-- 创建工单 -->
    <div v-if="showCreate" class="space-y-5">
      <!-- 步骤 1：选择服务类型 -->
      <div class="rounded-2xl border bg-card p-5 shadow-sm">
        <div class="flex items-start gap-3">
          <StepBadge :step="1" />
          <div>
            <h3 class="text-base font-bold text-foreground">{{ t('tickets.form.stepType') }}</h3>
          </div>
        </div>

        <div class="mt-4 grid gap-3 sm:grid-cols-2">
          <button
            v-for="option in typeOptions"
            :key="option.value"
            type="button"
            class="relative flex items-start gap-3 rounded-xl border-2 p-4 text-start transition-all"
            :class="
              createForm.ticketType === option.value
                ? 'border-primary bg-primary/5'
                : 'border-border hover:border-primary/40 hover:bg-muted/40'
            "
            @click="selectTicketType(option.value)"
          >
            <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-muted">
              <component :is="option.icon" class="h-5 w-5 text-muted-foreground" />
            </span>
            <span class="min-w-0 flex-1">
              <span class="block font-bold text-foreground">{{ option.label }}</span>
              <span class="mt-1 block text-xs text-muted-foreground">{{ option.description }}</span>
            </span>
            <CheckCircle2
              v-if="createForm.ticketType === option.value"
              class="h-5 w-5 shrink-0 text-primary"
            />
            <Circle v-else class="h-5 w-5 shrink-0 text-muted-foreground/40" />
          </button>
        </div>
      </div>

      <!-- 步骤 2：售前关联商品 -->
      <div v-if="createForm.ticketType === 'pre_sale'" class="rounded-2xl border bg-card p-5 shadow-sm">
        <div class="flex items-start gap-3">
          <StepBadge :step="2" />
          <div>
            <h3 class="text-base font-bold text-foreground">{{ t('tickets.form.stepProduct') }}</h3>
            <p class="mt-1 text-xs text-muted-foreground">{{ t('tickets.form.stepProductHint') }}</p>
          </div>
        </div>

        <div class="mt-4">
          <Label class="mb-1 block text-xs font-semibold text-muted-foreground">
            {{ t('tickets.form.relatedProduct') }}
            <span class="ml-1 font-normal text-muted-foreground">{{ t('tickets.form.optional') }}</span>
          </Label>
          <SearchSelect
            :model-value="createForm.productId"
            :options="productOptions"
            :loading="productLoading"
            :placeholder="t('tickets.form.productPlaceholder')"
            :search-placeholder="t('tickets.form.productSearchPlaceholder')"
            :empty-text="t('tickets.form.productEmpty')"
            :loading-text="t('common.loading')"
            :error-text="productError"
            @search="onProductSearch"
            @update:model-value="onProductPicked"
          >
            <template #selected="{ option }">
              <span class="flex min-w-0 items-center gap-2">
                <img v-if="option.image" :src="String(option.image)" alt="" class="h-6 w-6 rounded object-cover" />
                <span class="truncate">{{ option.label }}</span>
              </span>
            </template>
            <template #option="{ option }">
              <img
                v-if="option.image"
                :src="String(option.image)"
                alt=""
                class="h-9 w-9 shrink-0 rounded-md object-cover"
              />
              <span v-else class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted">
                <Package class="h-4 w-4 text-muted-foreground" />
              </span>
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm font-medium text-foreground">{{ option.label }}</span>
                <span v-if="option.description" class="mt-0.5 block truncate text-xs text-muted-foreground">
                  {{ option.description }}
                </span>
              </span>
            </template>
          </SearchSelect>
        </div>
      </div>

      <!-- 步骤 2：售后关联订单与凭证 -->
      <div v-else class="rounded-2xl border bg-card p-5 shadow-sm">
        <div class="flex items-start gap-3">
          <StepBadge :step="2" />
          <div>
            <h3 class="text-base font-bold text-foreground">{{ t('tickets.form.stepOrder') }}</h3>
            <p class="mt-1 text-xs text-muted-foreground">{{ t('tickets.form.stepOrderHint') }}</p>
          </div>
        </div>

        <div class="mt-4 inline-flex rounded-xl border bg-muted/40 p-1">
          <button
            type="button"
            class="rounded-lg px-3 py-1.5 text-sm font-semibold transition-colors"
            :class="orderMode === 'records' ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'"
            @click="switchOrderMode('records')"
          >
            {{ t('tickets.form.fromRecords') }}
          </button>
          <button
            type="button"
            class="rounded-lg px-3 py-1.5 text-sm font-semibold transition-colors"
            :class="orderMode === 'manual' ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'"
            @click="switchOrderMode('manual')"
          >
            {{ t('tickets.form.manualInput') }}
          </button>
        </div>

        <div class="mt-4 space-y-1">
          <Label class="block text-xs font-semibold text-muted-foreground">
            {{ t('tickets.form.relatedOrder') }}
            <span class="ml-0.5 text-destructive">*</span>
          </Label>

          <SearchSelect
            v-if="orderMode === 'records'"
            :model-value="createForm.orderNo || null"
            :options="orderOptions"
            :loading="orderLoading"
            :clearable="false"
            :placeholder="t('tickets.form.orderSearchPlaceholder')"
            :search-placeholder="t('tickets.form.orderSearchPlaceholder')"
            :empty-text="t('tickets.form.orderEmpty')"
            :loading-text="t('common.loading')"
            :error-text="orderError"
            @search="onOrderSearch"
            @update:model-value="onOrderPicked"
          >
            <template #selected="{ option }">
              <span class="min-w-0">
                <span class="block truncate text-sm font-medium">{{ option.label }}</span>
                <span v-if="option.description" class="block truncate text-xs text-muted-foreground">
                  {{ option.description }}
                </span>
              </span>
            </template>
            <template #option="{ option }">
              <img
                v-if="option.image"
                :src="String(option.image)"
                alt=""
                class="h-9 w-9 shrink-0 rounded-md object-cover"
              />
              <span v-else class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted">
                <ShoppingBag class="h-4 w-4 text-muted-foreground" />
              </span>
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm font-medium text-foreground">{{ option.label }}</span>
                <span v-if="option.description" class="mt-0.5 block truncate text-xs text-muted-foreground">
                  {{ option.description }}
                </span>
              </span>
            </template>
          </SearchSelect>

          <Input
            v-else
            :model-value="createForm.orderNo"
            :placeholder="t('tickets.form.manualOrderPlaceholder')"
            class="h-11"
            @update:model-value="(v) => (createForm.orderNo = String(v || ''))"
          />

          <p v-if="orderMode === 'manual'" class="text-xs text-muted-foreground">
            {{ t('tickets.form.manualOrderHint') }}
          </p>
          <button
            v-else
            type="button"
            class="text-xs text-primary hover:underline"
            @click="switchOrderMode('manual')"
          >
            {{ t('tickets.form.cannotFindOrder') }}
          </button>
        </div>

        <div class="mt-4">
          <Label class="mb-1 block text-xs font-semibold text-muted-foreground">
            {{ t('tickets.form.proof') }}
            <span class="ml-1 font-normal text-muted-foreground">{{ t('tickets.form.proofHint') }}</span>
          </Label>
          <TicketImageField v-model="createForm.imageUrl" />
        </div>
      </div>

      <!-- 步骤 3：问题描述 -->
      <div class="rounded-2xl border bg-card p-5 shadow-sm">
        <div class="flex items-start gap-3">
          <StepBadge :step="3" />
          <h3 class="text-base font-bold text-foreground">{{ t('tickets.form.stepDetail') }}</h3>
        </div>

        <div class="mt-4 space-y-4">
          <div>
            <Label class="mb-1 block text-xs font-semibold text-muted-foreground">{{ t('tickets.form.title') }}</Label>
            <Input v-model="createForm.title" :placeholder="t('tickets.form.titlePlaceholder')" class="h-11" />
          </div>
          <div>
            <Label class="mb-1 block text-xs font-semibold text-muted-foreground">{{ t('tickets.form.content') }}</Label>
            <Textarea v-model="createForm.content" rows="5" :placeholder="t('tickets.form.contentPlaceholder')" />
          </div>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <Label class="mb-1 block text-xs font-semibold text-muted-foreground">{{ t('tickets.form.priority') }}</Label>
              <Select v-model="createForm.priority">
                <SelectTrigger class="h-11 w-full"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="low">{{ t('tickets.priority.low') }}</SelectItem>
                  <SelectItem value="normal">{{ t('tickets.priority.normal') }}</SelectItem>
                  <SelectItem value="high">{{ t('tickets.priority.high') }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div v-if="createForm.ticketType === 'pre_sale'">
              <Label class="mb-1 block text-xs font-semibold text-muted-foreground">
                {{ t('tickets.form.image') }}
              </Label>
              <TicketImageField v-model="createForm.imageUrl" />
            </div>
          </div>
        </div>

        <div class="mt-5 flex items-center gap-2">
          <Button type="button" :disabled="submittingCreate" class="h-11 font-bold" @click="submitCreate">
            {{ t('tickets.form.submit') }}
          </Button>
          <Button type="button" variant="outline" class="h-11 font-semibold" @click="backToList">
            {{ t('tickets.form.cancel') }}
          </Button>
        </div>
      </div>
    </div>

    <!-- 工单详情 -->
    <div v-else-if="activeTicket" class="space-y-4">
      <div class="rounded-2xl border bg-card p-4 shadow-sm">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div>
            <div class="text-xs uppercase tracking-[0.16em] text-muted-foreground">{{ t('tickets.ticketNo') }}：{{ activeTicket.ticket_no }}</div>
            <div class="mt-1 text-lg font-bold text-foreground">{{ activeTicket.title }}</div>
          </div>
          <div class="flex items-center gap-2">
            <Badge variant="accent" size="sm">{{ typeLabel(activeTicket.ticket_type) }}</Badge>
            <Badge :variant="priorityVariant(activeTicket.priority)" size="sm">{{ priorityLabel(activeTicket.priority) }}</Badge>
            <Badge :variant="statusVariant(activeTicket.status)" size="sm">{{ statusLabel(activeTicket.status) }}</Badge>
          </div>
        </div>
        <div v-if="activeTicket.order_id" class="mt-2 text-xs text-muted-foreground">
          {{ t('tickets.relatedOrder') }}：{{ relatedOrderLabel(activeTicket) }}
        </div>
        <div v-else-if="activeTicket.product_id" class="mt-2 text-xs text-muted-foreground">
          {{ t('tickets.relatedProduct') }}：{{ relatedProductLabel(activeTicket) }}
        </div>
      </div>

      <div class="space-y-3 rounded-2xl border bg-card p-4 shadow-sm">
        <div
          v-for="message in activeTicket.messages"
          :key="message.id"
          class="max-w-[85%] rounded-2xl p-3"
          :class="message.sender_type === 'admin' ? 'bg-primary/10' : 'ml-auto bg-muted'"
        >
          <div class="mb-1 flex items-center justify-between gap-3 text-xs text-muted-foreground">
            <span class="font-semibold">{{ message.sender_type === 'admin' ? t('tickets.senderAdmin') : t('tickets.senderUser') }}</span>
            <span>{{ formatDate(message.created_at) }}</span>
          </div>
          <p class="whitespace-pre-wrap text-sm text-foreground">{{ message.content }}</p>
          <img v-if="message.image_url" :src="message.image_url" class="mt-2 max-h-56 rounded-lg border" />
        </div>
      </div>

      <div v-if="activeTicket.status !== 'closed'" class="rounded-2xl border bg-card p-4 shadow-sm space-y-3">
        <Textarea v-model="replyContent" rows="4" :placeholder="t('tickets.replyPlaceholder')" />
        <TicketImageField v-model="replyImageUrl" />
        <Button type="button" :disabled="submittingReply" class="h-11 font-bold" @click="submitReply">
          {{ t('tickets.reply') }}
        </Button>
      </div>
      <div v-else class="rounded-2xl border border-dashed p-4 text-center text-sm text-muted-foreground">
        {{ t('tickets.closedHint') }}
      </div>
    </div>

    <!-- 工单列表 -->
    <template v-else>
      <div class="rounded-2xl border bg-card p-4 shadow-sm">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div class="w-full sm:w-56">
            <Label class="mb-1 block text-xs font-semibold text-muted-foreground">{{ t('tickets.filterStatus') }}</Label>
            <Select v-model="statusProxy">
              <SelectTrigger class="h-11 w-full"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{{ t('tickets.filterStatusAll') }}</SelectItem>
                <SelectItem value="open">{{ t('tickets.status.open') }}</SelectItem>
                <SelectItem value="replied">{{ t('tickets.status.replied') }}</SelectItem>
                <SelectItem value="closed">{{ t('tickets.status.closed') }}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>

      <div v-if="loading" class="space-y-4">
        <div v-for="i in 3" :key="i" class="h-24 animate-pulse rounded-2xl border bg-muted"></div>
      </div>

      <EmptyState
        v-else-if="tickets.length === 0"
        icon="inbox"
        :description="t('tickets.empty')"
        :action-label="t('tickets.create')"
        @action="openCreate"
      />

      <div v-else class="space-y-4">
        <div
          v-for="ticket in tickets"
          :key="ticket.id"
          class="cursor-pointer rounded-2xl border bg-card p-6 shadow-sm transition-all transition hover:-translate-y-0.5 hover:border-primary/30 hover:shadow-md"
          @click="openDetail(ticket)"
        >
          <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div>
              <div class="text-xs uppercase tracking-[0.16em] text-muted-foreground">{{ t('tickets.ticketNo') }}：{{ ticket.ticket_no }}</div>
              <div class="mt-2 text-base font-bold text-foreground">{{ ticket.title }}</div>
              <div class="mt-2 text-xs text-muted-foreground">{{ formatDate(ticket.updated_at) }}</div>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <Badge variant="accent" size="sm">{{ typeLabel(ticket.ticket_type) }}</Badge>
              <Badge :variant="priorityVariant(ticket.priority)" size="sm">{{ priorityLabel(ticket.priority) }}</Badge>
              <Badge :variant="statusVariant(ticket.status)" size="sm">{{ statusLabel(ticket.status) }}</Badge>
            </div>
          </div>
        </div>
      </div>

      <PaginationNav
        :current-page="pagination.page"
        :total-pages="pagination.total_page"
        :loading="loading"
        :scroll-top="false"
        @change-page="changePage"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { CheckCircle2, Circle, MessageSquare, Package, ShoppingBag, Wrench } from 'lucide-vue-next'
import { productAPI, userOrderAPI, userTicketAPI } from '../../api'
import type { Ticket } from '../../api/types'
import type { BadgeTone } from '../../utils/status'
import { getLocalizedText } from '../../utils/resellerSiteConfig'
import { getImageUrl } from '../../utils/image'
import { useAppStore } from '../../stores/app'
import { debounceAsync } from '../../utils/debounce'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Alert, AlertDescription } from '@/components/ui/alert'
import EmptyState from '../../components/EmptyState.vue'
import PaginationNav from '../../components/PaginationNav.vue'
import PanelHeading from '../../components/shared/PanelHeading.vue'
import SearchSelect, { type SearchSelectOption } from '../../components/SearchSelect.vue'
import TicketImageField from './TicketImageField.vue'
import { pageAlertVariant, pageAlertToneClass, type PageAlert } from '../../utils/alerts'

const { t } = useI18n()
const appStore = useAppStore()
const alert = ref<PageAlert | null>(null)

/** 步骤序号圆点，仅用于视觉分层 */
const StepBadge = defineComponent({
  props: { step: { type: Number, required: true } },
  setup(props) {
    return () =>
      h(
        'span',
        {
          class: 'flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-foreground text-xs font-bold text-background',
        },
        String(props.step),
      )
  },
})

const loading = ref(true)
const tickets = ref<Ticket[]>([])
const pagination = ref({ page: 1, page_size: 20, total: 0, total_page: 1 })
const filters = ref({ status: '' })

const loadTickets = async (page = 1) => {
  loading.value = true
  try {
    const response = await userTicketAPI.list({
      page,
      page_size: pagination.value.page_size,
      status: filters.value.status || undefined,
    })
    tickets.value = response.data.data || []
    pagination.value = response.data.pagination || pagination.value
  } catch {
    tickets.value = []
  } finally {
    loading.value = false
  }
}

const changePage = (page: number) => {
  if (page < 1 || page > pagination.value.total_page) return
  loadTickets(page)
}

const statusProxy = computed({
  get: () => filters.value.status || 'all',
  set: (v: string) => {
    filters.value.status = v === 'all' ? '' : v
    loadTickets(1)
  },
})

// ========== 创建工单 ==========
const showCreate = ref(false)
const submittingCreate = ref(false)

const createForm = ref({
  ticketType: 'pre_sale' as 'pre_sale' | 'after_sale',
  productId: null as number | null,
  orderNo: '',
  title: '',
  content: '',
  priority: 'normal',
  imageUrl: '',
})

const orderMode = ref<'records' | 'manual'>('records')

const typeOptions = computed(() => [
  {
    value: 'pre_sale' as const,
    label: t('tickets.form.typePreSale'),
    description: t('tickets.form.typePreSaleHint'),
    icon: MessageSquare,
  },
  {
    value: 'after_sale' as const,
    label: t('tickets.form.typeAfterSale'),
    description: t('tickets.form.typeAfterSaleHint'),
    icon: Wrench,
  },
])

const openCreate = () => {
  activeTicket.value = null
  showCreate.value = true
  alert.value = null
  createForm.value = {
    ticketType: 'pre_sale',
    productId: null,
    orderNo: '',
    title: '',
    content: '',
    priority: 'normal',
    imageUrl: '',
  }
  orderMode.value = 'records'
  productOptions.value = []
  orderOptions.value = []
}

const backToList = () => {
  showCreate.value = false
  activeTicket.value = null
  alert.value = null
  loadTickets(pagination.value.page)
}

/** 切换服务类型时清理不适用于新类型的关联字段，避免带着旧关联误提交 */
const selectTicketType = (next: 'pre_sale' | 'after_sale') => {
  if (createForm.value.ticketType === next) return
  createForm.value.ticketType = next
  createForm.value.productId = null
  createForm.value.orderNo = ''
  createForm.value.imageUrl = ''
  alert.value = null
}

const switchOrderMode = (mode: 'records' | 'manual') => {
  if (orderMode.value === mode) return
  orderMode.value = mode
  // 切换输入方式时清掉上一种方式选中的订单，避免残留旧订单号
  createForm.value.orderNo = ''
  if (mode === 'records') loadRecentOrders('')
}

const onOrderPicked = (value: string | number | null) => {
  createForm.value.orderNo = value === null || value === undefined ? '' : String(value)
}

// ========== 商品搜索选择 ==========
const productOptions = ref<SearchSelectOption[]>([])
const productLoading = ref(false)
const productError = ref('')

const mapProductOption = (product: any): SearchSelectOption => {
  const title = getLocalizedText(product?.title, appStore.locale) || `#${product?.id ?? ''}`
  const image = getImageUrl(Array.isArray(product?.images) ? product.images[0] : '')
  return {
    value: product?.id,
    label: title,
    description: product?.slug
      ? `${product.slug} · ${t('tickets.form.productIdLabel')} ${product.id}`
      : `${t('tickets.form.productIdLabel')} ${product?.id ?? ''}`,
    image,
  }
}

const onProductPicked = (value: string | number | null) => {
  createForm.value.productId = value === null || value === undefined ? null : Number(value)
}

const loadProducts = async (keyword = '') => {
  productLoading.value = true
  productError.value = ''
  try {
    const response = await productAPI.list({ page: 1, page_size: 20, search: keyword || undefined })
    productOptions.value = (response.data.data || []).map(mapProductOption)
  } catch (err: any) {
    productOptions.value = []
    productError.value = err?.response?.data?.msg || t('tickets.form.productLoadFailed')
  } finally {
    productLoading.value = false
  }
}

const onProductSearch = debounceAsync(async (keyword: string) => {
  await loadProducts(keyword)
}, 300)

// ========== 订单搜索选择 ==========
const orderOptions = ref<SearchSelectOption[]>([])
const orderLoading = ref(false)
const orderError = ref('')

const mapOrderOption = (order: any): SearchSelectOption => {
  const items = Array.isArray(order?.items) ? order.items : []
  const titles = items
    .map((item: any) => getLocalizedText(item?.title, appStore.locale))
    .filter((item: string) => item)
  const summary = titles.length > 0 ? titles.join('、') : t('tickets.form.orderNoItems')
  const amount = order?.total_amount ? `${order.total_amount} ${order.currency || ''}`.trim() : ''
  const created = formatDate(order?.created_at)
  return {
    value: order?.order_no,
    label: summary,
    description: [order?.order_no, amount, created].filter((item: string) => item).join(' · '),
  }
}

const loadRecentOrders = async (keyword = '') => {
  orderLoading.value = true
  orderError.value = ''
  try {
    const response = await userOrderAPI.list({ page: 1, page_size: 20, order_no: keyword || undefined })
    orderOptions.value = (response.data.data || []).map(mapOrderOption)
  } catch (err: any) {
    orderOptions.value = []
    orderError.value = err?.response?.data?.msg || t('tickets.form.orderLoadFailed')
  } finally {
    orderLoading.value = false
  }
}

const onOrderSearch = debounceAsync(async (keyword: string) => {
  await loadRecentOrders(keyword)
}, 300)

const submitCreate = async () => {
  if (!createForm.value.title.trim() || !createForm.value.content.trim()) {
    alert.value = { level: 'error', message: t('tickets.form.required') }
    return
  }
  if (createForm.value.ticketType === 'after_sale' && !createForm.value.orderNo.trim()) {
    alert.value = { level: 'error', message: t('tickets.form.orderRequired') }
    return
  }
  submittingCreate.value = true
  try {
    await userTicketAPI.create({
      ticket_type: createForm.value.ticketType,
      title: createForm.value.title.trim(),
      content: createForm.value.content.trim(),
      priority: createForm.value.priority,
      product_id:
        createForm.value.ticketType === 'pre_sale' && createForm.value.productId
          ? createForm.value.productId
          : undefined,
      order_no: createForm.value.ticketType === 'after_sale' ? createForm.value.orderNo.trim() : undefined,
      image_url: createForm.value.imageUrl || undefined,
    })
    alert.value = { level: 'success', message: t('tickets.form.createSuccess') }
    showCreate.value = false
    loadTickets(1)
  } catch (err: any) {
    alert.value = { level: 'error', message: err?.response?.data?.msg || t('tickets.form.createFailed') }
  } finally {
    submittingCreate.value = false
  }
}

// ========== 详情/回复 ==========
const activeTicket = ref<Ticket | null>(null)
const replyContent = ref('')
const replyImageUrl = ref('')
const submittingReply = ref(false)

const openDetail = async (ticket: Ticket) => {
  showCreate.value = false
  replyContent.value = ''
  replyImageUrl.value = ''
  alert.value = null
  try {
    const response = await userTicketAPI.detail(ticket.id)
    activeTicket.value = response.data.data
  } catch {
    activeTicket.value = null
  }
}

const submitReply = async () => {
  if (!activeTicket.value) return
  const content = replyContent.value.trim()
  if (!content) {
    alert.value = { level: 'error', message: t('tickets.replyRequired') }
    return
  }
  submittingReply.value = true
  try {
    await userTicketAPI.reply(activeTicket.value.id, { content, image_url: replyImageUrl.value || undefined })
    alert.value = null
    replyContent.value = ''
    replyImageUrl.value = ''
    await openDetail(activeTicket.value)
  } catch (err: any) {
    alert.value = { level: 'error', message: err?.response?.data?.msg || t('tickets.replyFailed') }
  } finally {
    submittingReply.value = false
  }
}

// ========== 展示工具 ==========
const statusLabel = (status: string) => {
  const map: Record<string, string> = {
    open: t('tickets.status.open'),
    replied: t('tickets.status.replied'),
    closed: t('tickets.status.closed'),
  }
  return map[status] || status
}
const statusVariant = (status: string): BadgeTone => {
  if (status === 'open') return 'warning'
  if (status === 'replied') return 'success'
  return 'neutral'
}
const priorityLabel = (priority: string) => {
  const map: Record<string, string> = {
    low: t('tickets.priority.low'),
    normal: t('tickets.priority.normal'),
    high: t('tickets.priority.high'),
  }
  return map[priority] || priority
}
const priorityVariant = (priority: string): BadgeTone => {
  if (priority === 'high') return 'danger'
  if (priority === 'low') return 'neutral'
  return 'info'
}
/** 历史工单没有 ticket_type，按后端兼容默认值展示为售后 */
const typeLabel = (type?: string) =>
  type === 'pre_sale' ? t('tickets.form.typePreSale') : t('tickets.form.typeAfterSale')

/** 关联订单展示：优先订单号，后端未解析出来时回退到订单 ID */
const relatedOrderLabel = (ticket: Ticket) =>
  ticket.order_no || `#${ticket.order_id}`

/** 关联商品展示：优先商品名，商品已删除或未解析时回退到商品 ID */
const relatedProductLabel = (ticket: Ticket) => {
  const title = getLocalizedText(ticket.product_title, appStore.locale)
  return title || `#${ticket.product_id}`
}


const formatDate = (raw?: string) => {
  if (!raw) return ''
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return raw
  return date.toLocaleString()
}

onMounted(() => {
  loadTickets(1)
})

onUnmounted(() => {
  onProductSearch.cancel()
  onOrderSearch.cancel()
})
</script>
