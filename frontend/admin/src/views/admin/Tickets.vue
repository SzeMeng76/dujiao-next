<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminTicket } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Dialog, DialogScrollContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import TableSkeleton from '@/components/TableSkeleton.vue'
import ListPagination from '@/components/ListPagination.vue'
import { useListRefresh } from '@/composables/useListRefresh'
import { useAdminAuthStore } from '@/stores/auth'
import { formatDate } from '@/utils/format'
import { notifyError, notifySuccess } from '@/utils/notify'
import { confirmAction } from '@/utils/confirm'

const { t } = useI18n()
const authStore = useAdminAuthStore()
const loading = ref(true)
const { refreshing, refreshList } = useListRefresh()

const tickets = ref<AdminTicket[]>([])
const pagination = ref({ page: 1, page_size: 20, total: 0, total_page: 1 })
const filters = reactive({ status: '', priority: '', keyword: '' })

const normalizeFilterValue = (value: string) => (value === '__all__' ? '' : value)

const fetchTickets = async (page = 1) => {
  loading.value = true
  try {
    const response = await adminAPI.getTickets({
      page,
      page_size: pagination.value.page_size,
      status: normalizeFilterValue(filters.status) || undefined,
      priority: normalizeFilterValue(filters.priority) || undefined,
      keyword: filters.keyword || undefined,
    })
    tickets.value = response.data.data || []
    pagination.value = response.data.pagination || pagination.value
  } catch {
    tickets.value = []
  } finally {
    loading.value = false
  }
}

const handleSearch = () => fetchTickets(1)
const debouncedSearch = useDebounceFn(handleSearch, 300)
const refresh = () => refreshList(() => fetchTickets(pagination.value.page))
const changePage = (page: number) => {
  if (page < 1 || page > pagination.value.total_page) return
  fetchTickets(page)
}
const pageSizeOptions = [10, 20, 50, 100]
const changePageSize = (size: number) => {
  if (size === pagination.value.page_size) return
  pagination.value.page_size = size
  fetchTickets(1)
}

// ========== 批量删除（仅超管） ==========
const batchMode = ref(false)
const batchOperating = ref(false)
const selectedIds = ref<Set<number>>(new Set())

const allSelected = computed(() => tickets.value.length > 0 && tickets.value.every((t) => selectedIds.value.has(t.id)))

const toggleBatchMode = () => {
  batchMode.value = !batchMode.value
  if (!batchMode.value) selectedIds.value = new Set()
}
const toggleSelect = (id: number) => {
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}
const toggleSelectAll = () => {
  if (allSelected.value) {
    selectedIds.value = new Set()
  } else {
    selectedIds.value = new Set(tickets.value.map((t) => t.id))
  }
}
const clearSelection = () => {
  selectedIds.value = new Set()
}

const handleBatchDelete = async () => {
  const ids = Array.from(selectedIds.value)
  if (ids.length === 0) return
  const confirmed = await confirmAction(t('admin.tickets.batch.deleteConfirm', { count: ids.length }))
  if (!confirmed) return
  batchOperating.value = true
  try {
    const res = await adminAPI.batchDeleteTickets(ids)
    const deletedCount = res.data?.data?.deleted_count ?? ids.length
    notifySuccess(t('admin.tickets.batch.deleteResult', { success: deletedCount, total: ids.length }))
    clearSelection()
    fetchTickets(pagination.value.page)
  } catch (err: any) {
    notifyError(t('admin.tickets.errors.deleteFailed', { message: err?.response?.data?.msg || '' }))
  } finally {
    batchOperating.value = false
  }
}

const statusLabel = (status: string) => {
  const map: Record<string, string> = {
    open: t('admin.tickets.status.open'),
    replied: t('admin.tickets.status.replied'),
    closed: t('admin.tickets.status.closed'),
  }
  return map[status] || status
}
const statusClass = (status: string) => {
  if (status === 'open') return 'text-amber-700 border-amber-200 bg-amber-50'
  if (status === 'replied') return 'text-emerald-700 border-emerald-200 bg-emerald-50'
  return 'text-slate-600 border-slate-200 bg-slate-50'
}
const priorityLabel = (priority: string) => {
  const map: Record<string, string> = {
    low: t('admin.tickets.priority.low'),
    normal: t('admin.tickets.priority.normal'),
    high: t('admin.tickets.priority.high'),
  }
  return map[priority] || priority
}
const priorityClass = (priority: string) => {
  if (priority === 'high') return 'text-rose-700 border-rose-200 bg-rose-50'
  if (priority === 'low') return 'text-slate-600 border-slate-200 bg-slate-50'
  return 'text-blue-700 border-blue-200 bg-blue-50'
}

// ========== 详情/回复 ==========
const showDetail = ref(false)
const detailLoading = ref(false)
const detail = ref<AdminTicket | null>(null)
const replyContent = ref('')
const replying = ref(false)
const closing = ref(false)

const openDetail = async (ticket: AdminTicket) => {
  showDetail.value = true
  detailLoading.value = true
  replyContent.value = ''
  try {
    const response = await adminAPI.getTicket(ticket.id)
    detail.value = response.data.data
  } catch {
    detail.value = null
  } finally {
    detailLoading.value = false
  }
}

const canReply = computed(() => detail.value && detail.value.status !== 'closed')

const senderLabel = (senderType: string) => {
  return senderType === 'admin' ? t('admin.tickets.senderAdmin') : t('admin.tickets.senderUser')
}

const submitReply = async (closeAfter: boolean) => {
  if (!detail.value) return
  const content = replyContent.value.trim()
  if (!content) {
    notifyError(t('admin.tickets.replyRequired'))
    return
  }
  replying.value = true
  try {
    await adminAPI.replyTicket(detail.value.id, { content, close: closeAfter })
    notifySuccess(t('admin.tickets.replySuccess'))
    replyContent.value = ''
    await openDetail(detail.value)
    fetchTickets(pagination.value.page)
  } catch (err: any) {
    notifyError(err?.response?.data?.msg || t('admin.tickets.replyFailed'))
  } finally {
    replying.value = false
  }
}

const closeTicket = async () => {
  if (!detail.value) return
  closing.value = true
  try {
    await adminAPI.closeTicket(detail.value.id)
    notifySuccess(t('admin.tickets.closeSuccess'))
    await openDetail(detail.value)
    fetchTickets(pagination.value.page)
  } catch (err: any) {
    notifyError(err?.response?.data?.msg || t('admin.tickets.closeFailed'))
  } finally {
    closing.value = false
  }
}

const reopening = ref(false)
const reopenTicket = async () => {
  if (!detail.value) return
  reopening.value = true
  try {
    await adminAPI.reopenTicket(detail.value.id)
    notifySuccess(t('admin.tickets.reopenSuccess'))
    await openDetail(detail.value)
    fetchTickets(pagination.value.page)
  } catch (err: any) {
    notifyError(err?.response?.data?.msg || t('admin.tickets.reopenFailed'))
  } finally {
    reopening.value = false
  }
}

onMounted(() => {
  fetchTickets()
})
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <h1 class="text-2xl font-semibold">{{ t('admin.tickets.title') }}</h1>
      <div class="flex items-center gap-2">
        <Button
          v-if="authStore.isSuper"
          size="sm"
          :variant="batchMode ? 'secondary' : 'outline'"
          :disabled="loading"
          @click="toggleBatchMode"
        >
          {{ batchMode ? t('admin.tickets.batch.exit') : t('admin.tickets.batch.mode') }}
        </Button>
        <Button variant="outline" size="sm" :disabled="refreshing" @click="refresh">
          {{ t('admin.common.refresh') }}
        </Button>
      </div>
    </div>

    <div v-if="batchMode" class="flex flex-wrap items-center gap-3 rounded-lg border border-primary/20 bg-primary/5 px-4 py-3">
      <span class="text-sm font-medium">{{ t('admin.tickets.batch.selected', { count: selectedIds.size }) }}</span>
      <Button size="sm" variant="outline" :disabled="tickets.length === 0 || batchOperating" @click="toggleSelectAll">
        {{ allSelected ? t('admin.tickets.batch.deselectPage') : t('admin.tickets.batch.selectPage') }}
      </Button>
      <Button size="sm" variant="destructive" :disabled="selectedIds.size === 0 || batchOperating" @click="handleBatchDelete">
        {{ t('admin.tickets.batch.delete') }}
      </Button>
      <button class="ml-auto text-xs text-muted-foreground hover:text-foreground" :disabled="batchOperating" @click="clearSelection">
        {{ t('admin.tickets.batch.clearSelection') }}
      </button>
    </div>

    <div class="rounded-xl border border-border bg-card p-4 shadow-sm">
      <div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
        <div class="w-full md:w-56">
          <Input v-model="filters.keyword" :placeholder="t('admin.tickets.filterKeyword')" @update:modelValue="debouncedSearch" />
        </div>
        <div class="w-full md:w-40">
          <Select v-model="filters.status" @update:modelValue="handleSearch">
            <SelectTrigger class="h-10 w-full"><SelectValue :placeholder="t('admin.tickets.filterStatus')" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{{ t('admin.tickets.filterStatusAll') }}</SelectItem>
              <SelectItem value="open">{{ t('admin.tickets.status.open') }}</SelectItem>
              <SelectItem value="replied">{{ t('admin.tickets.status.replied') }}</SelectItem>
              <SelectItem value="closed">{{ t('admin.tickets.status.closed') }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div class="w-full md:w-40">
          <Select v-model="filters.priority" @update:modelValue="handleSearch">
            <SelectTrigger class="h-10 w-full"><SelectValue :placeholder="t('admin.tickets.filterPriority')" /></SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{{ t('admin.tickets.filterPriorityAll') }}</SelectItem>
              <SelectItem value="low">{{ t('admin.tickets.priority.low') }}</SelectItem>
              <SelectItem value="normal">{{ t('admin.tickets.priority.normal') }}</SelectItem>
              <SelectItem value="high">{{ t('admin.tickets.priority.high') }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
    </div>

    <div class="rounded-xl border border-border bg-card shadow-sm">
      <TableSkeleton v-if="loading" :rows="6" :columns="6" />
      <Table v-else>
        <TableHeader>
          <TableRow>
            <TableHead v-if="batchMode" class="w-10"></TableHead>
            <TableHead>{{ t('admin.tickets.columns.ticketNo') }}</TableHead>
            <TableHead>{{ t('admin.tickets.columns.title') }}</TableHead>
            <TableHead>{{ t('admin.tickets.columns.user') }}</TableHead>
            <TableHead>{{ t('admin.tickets.columns.priority') }}</TableHead>
            <TableHead>{{ t('admin.tickets.columns.status') }}</TableHead>
            <TableHead>{{ t('admin.tickets.columns.updatedAt') }}</TableHead>
            <TableHead>{{ t('admin.tickets.columns.actions') }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          <TableRow v-if="tickets.length === 0">
            <TableCell :colspan="batchMode ? 8 : 7" class="text-center text-sm text-muted-foreground py-8">
              {{ t('admin.tickets.empty') }}
            </TableCell>
          </TableRow>
          <TableRow v-for="ticket in tickets" :key="ticket.id">
            <TableCell v-if="batchMode">
              <Checkbox :model-value="selectedIds.has(ticket.id)" @update:model-value="() => toggleSelect(ticket.id)" />
            </TableCell>
            <TableCell class="font-mono text-xs">{{ ticket.ticket_no }}</TableCell>
            <TableCell class="max-w-xs truncate">{{ ticket.title }}</TableCell>
            <TableCell class="text-sm">{{ ticket.user_display_name || ticket.user_email || `#${ticket.user_id}` }}</TableCell>
            <TableCell><span class="inline-flex rounded-full border px-2 py-0.5 text-xs" :class="priorityClass(ticket.priority)">{{ priorityLabel(ticket.priority) }}</span></TableCell>
            <TableCell><span class="inline-flex rounded-full border px-2 py-0.5 text-xs" :class="statusClass(ticket.status)">{{ statusLabel(ticket.status) }}</span></TableCell>
            <TableCell class="text-xs text-muted-foreground">{{ formatDate(ticket.updated_at) }}</TableCell>
            <TableCell>
              <Button variant="outline" size="sm" @click="openDetail(ticket)">{{ t('admin.tickets.viewDetail') }}</Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>

      <ListPagination
        :page="pagination.page"
        :total-page="pagination.total_page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        :page-size-options="pageSizeOptions"
        @change-page="changePage"
        @change-page-size="changePageSize"
      />
    </div>

    <Dialog :open="showDetail" @update:open="(v) => { showDetail = v }">
      <DialogScrollContent class="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{{ detail ? `${t('admin.tickets.detailTitle')} - ${detail.ticket_no}` : t('admin.tickets.detailTitle') }}</DialogTitle>
        </DialogHeader>

        <div v-if="detailLoading" class="py-8 text-center text-sm text-muted-foreground">{{ t('admin.common.loading') }}</div>

        <div v-else-if="detail" class="space-y-4">
          <div class="grid grid-cols-2 gap-3 text-sm">
            <div><span class="text-muted-foreground">{{ t('admin.tickets.columns.title') }}：</span>{{ detail.title }}</div>
            <div><span class="text-muted-foreground">{{ t('admin.tickets.columns.user') }}：</span>{{ detail.user_display_name || detail.user_email || `#${detail.user_id}` }}</div>
            <div>
              <span class="text-muted-foreground">{{ t('admin.tickets.columns.priority') }}：</span>
              <span class="inline-flex rounded-full border px-2 py-0.5 text-xs" :class="priorityClass(detail.priority)">{{ priorityLabel(detail.priority) }}</span>
            </div>
            <div>
              <span class="text-muted-foreground">{{ t('admin.tickets.columns.status') }}：</span>
              <span class="inline-flex rounded-full border px-2 py-0.5 text-xs" :class="statusClass(detail.status)">{{ statusLabel(detail.status) }}</span>
            </div>
            <div v-if="detail.order_id"><span class="text-muted-foreground">{{ t('admin.tickets.relatedOrder') }}：</span>#{{ detail.order_id }}</div>
          </div>

          <div class="max-h-80 space-y-3 overflow-y-auto rounded-lg border border-border bg-muted/20 p-4">
            <div
              v-for="message in detail.messages"
              :key="message.id"
              class="rounded-lg p-3"
              :class="message.sender_type === 'admin' ? 'bg-primary/10 ml-6' : 'bg-card mr-6'"
            >
              <div class="mb-1 flex items-center justify-between text-xs text-muted-foreground">
                <span class="font-semibold">{{ senderLabel(message.sender_type) }}</span>
                <span>{{ formatDate(message.created_at) }}</span>
              </div>
              <p class="whitespace-pre-wrap text-sm">{{ message.content }}</p>
              <img v-if="message.image_url" :src="message.image_url" class="mt-2 max-h-48 rounded border border-border" />
            </div>
          </div>

          <div v-if="canReply" class="space-y-2">
            <Textarea v-model="replyContent" rows="4" :placeholder="t('admin.tickets.replyPlaceholder')" />
            <div class="flex flex-wrap items-center gap-2">
              <Button :disabled="replying" @click="submitReply(false)">{{ t('admin.tickets.reply') }}</Button>
              <Button variant="outline" :disabled="replying" @click="submitReply(true)">{{ t('admin.tickets.replyAndClose') }}</Button>
              <Button variant="ghost" :disabled="closing" @click="closeTicket">{{ t('admin.tickets.closeOnly') }}</Button>
            </div>
          </div>
          <div v-else class="space-y-3 rounded-lg border border-dashed border-border p-3 text-center text-sm text-muted-foreground">
            <p>{{ t('admin.tickets.closedHint') }}</p>
            <Button variant="outline" size="sm" :disabled="reopening" @click="reopenTicket">{{ t('admin.tickets.reopen') }}</Button>
          </div>
        </div>
      </DialogScrollContent>
    </Dialog>
  </div>
</template>
