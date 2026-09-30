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
    <div v-if="showCreate" class="rounded-2xl border bg-card p-4 shadow-sm space-y-4">
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
        <div>
          <Label class="mb-1 block text-xs font-semibold text-muted-foreground">{{ t('tickets.form.orderNo') }}</Label>
          <Input v-model="createForm.orderNo" :placeholder="t('tickets.form.orderNoPlaceholder')" class="h-11" />
        </div>
      </div>
      <div>
        <Label class="mb-1 block text-xs font-semibold text-muted-foreground">{{ t('tickets.form.image') }}</Label>
        <TicketImageField v-model="createForm.imageUrl" />
      </div>
      <div class="flex items-center gap-2">
        <Button type="button" :disabled="submittingCreate" class="h-11 font-bold" @click="submitCreate">
          {{ t('tickets.form.submit') }}
        </Button>
        <Button type="button" variant="outline" class="h-11 font-semibold" @click="backToList">
          {{ t('tickets.form.cancel') }}
        </Button>
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
            <Badge :variant="priorityVariant(activeTicket.priority)" size="sm">{{ priorityLabel(activeTicket.priority) }}</Badge>
            <Badge :variant="statusVariant(activeTicket.status)" size="sm">{{ statusLabel(activeTicket.status) }}</Badge>
          </div>
        </div>
        <div v-if="activeTicket.order_id" class="mt-2 text-xs text-muted-foreground">
          {{ t('tickets.relatedOrder') }}：#{{ activeTicket.order_id }}
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
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageSquare } from 'lucide-vue-next'
import { userTicketAPI } from '../../api'
import type { Ticket } from '../../api/types'
import type { BadgeTone } from '../../utils/status'
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
import TicketImageField from './TicketImageField.vue'
import { pageAlertVariant, pageAlertToneClass, type PageAlert } from '../../utils/alerts'

const { t } = useI18n()
const alert = ref<PageAlert | null>(null)

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
const createForm = ref({ title: '', content: '', priority: 'normal', orderNo: '', imageUrl: '' })

const openCreate = () => {
  activeTicket.value = null
  showCreate.value = true
  alert.value = null
  createForm.value = { title: '', content: '', priority: 'normal', orderNo: '', imageUrl: '' }
}

const backToList = () => {
  showCreate.value = false
  activeTicket.value = null
  alert.value = null
  loadTickets(pagination.value.page)
}

const submitCreate = async () => {
  if (!createForm.value.title.trim() || !createForm.value.content.trim()) {
    alert.value = { level: 'error', message: t('tickets.form.required') }
    return
  }
  submittingCreate.value = true
  try {
    await userTicketAPI.create({
      title: createForm.value.title.trim(),
      content: createForm.value.content.trim(),
      priority: createForm.value.priority,
      order_no: createForm.value.orderNo.trim() || undefined,
      image_url: createForm.value.imageUrl || undefined,
    })
    alert.value = null
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
const formatDate = (raw?: string) => {
  if (!raw) return ''
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return raw
  return date.toLocaleString()
}

onMounted(() => {
  loadTickets(1)
})
</script>
