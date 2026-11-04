<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { adminAPI, type AdminErrorLog } from '@/api/admin'
import IdCell from '@/components/IdCell.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import TableSkeleton from '@/components/TableSkeleton.vue'
import ListPagination from '@/components/ListPagination.vue'
import { useListRefresh, type ListFetchOptions } from '@/composables/useListRefresh'
import { formatDate, toRFC3339 } from '@/utils/format'

const { t } = useI18n()
const loading = ref(true)
const { refreshing, refreshList } = useListRefresh()
const logs = ref<AdminErrorLog[]>([])
const selectedLog = ref<AdminErrorLog | null>(null)

const pagination = ref({
  page: 1,
  page_size: 20,
  total: 0,
  total_page: 1,
})

const filters = reactive({
  level: '__all__',
  category: '__all__',
  method: '__all__',
  path: '',
  requestId: '',
  clientIp: '',
  keyword: '',
  createdFrom: '',
  createdTo: '',
})

const normalizeFilter = (v: string) => (v === '__all__' ? '' : v)

const levels = ['error', 'warn']
const categories = ['payment', 'order', 'upstream', 'channel', 'auth', 'system']
const methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']

const fetchLogs = async (page = 1, options: ListFetchOptions = {}) => {
  if (!options.preserveRows) loading.value = true
  try {
    const response = await adminAPI.getErrorLogs({
      page,
      page_size: pagination.value.page_size,
      level: normalizeFilter(filters.level) || undefined,
      category: normalizeFilter(filters.category) || undefined,
      method: normalizeFilter(filters.method) || undefined,
      path: filters.path || undefined,
      request_id: filters.requestId || undefined,
      client_ip: filters.clientIp || undefined,
      keyword: filters.keyword || undefined,
      created_from: toRFC3339(filters.createdFrom),
      created_to: toRFC3339(filters.createdTo),
    })
    logs.value = response.data.data || []
    pagination.value = response.data.pagination || pagination.value
  } catch {
    if (!options.preserveRows) logs.value = []
  } finally {
    if (!options.preserveRows) loading.value = false
  }
}

const handleSearch = () => fetchLogs(1)
const debouncedSearch = useDebounceFn(handleSearch, 300)
const refresh = () => refreshList(() => fetchLogs(pagination.value.page, { preserveRows: true }))
const changePage = (page: number) => {
  if (page < 1 || page > pagination.value.total_page) return
  fetchLogs(page)
}
const pageSizeOptions = [10, 20, 50, 100]
const changePageSize = (size: number) => {
  if (size === pagination.value.page_size) return
  pagination.value.page_size = size
  fetchLogs(1)
}

const levelClass = (level: string) => {
  if (level === 'error') return 'border-rose-500/30 bg-rose-500/10 text-rose-600 dark:text-rose-300'
  return 'border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-300'
}

const levelLabel = (level: string) => t(`admin.errorLogs.level.${level}`) || level

const categoryLabel = (category: string) => {
  const key = `admin.errorLogs.category.${category}`
  const translated = t(key)
  return translated === key ? category : translated
}

const openDetail = (log: AdminErrorLog) => {
  selectedLog.value = log
}

onMounted(() => fetchLogs())
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-semibold">{{ t('admin.errorLogs.title') }}</h1>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('admin.errorLogs.subtitle') }}</p>
      </div>
    </div>

    <!-- 筛选栏 -->
    <div class="rounded-xl border border-border bg-card p-4 shadow-sm">
      <div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
        <!-- 级别 -->
        <div class="w-full md:w-32">
          <Select v-model="filters.level" @update:modelValue="handleSearch">
            <SelectTrigger class="h-9 w-full">
              <SelectValue :placeholder="t('admin.errorLogs.filterLevel')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{{ t('admin.errorLogs.filterAll') }}</SelectItem>
              <SelectItem v-for="l in levels" :key="l" :value="l">{{ levelLabel(l) }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <!-- 分类 -->
        <div class="w-full md:w-32">
          <Select v-model="filters.category" @update:modelValue="handleSearch">
            <SelectTrigger class="h-9 w-full">
              <SelectValue :placeholder="t('admin.errorLogs.filterCategory')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{{ t('admin.errorLogs.filterAll') }}</SelectItem>
              <SelectItem v-for="c in categories" :key="c" :value="c">{{ categoryLabel(c) }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <!-- 方法 -->
        <div class="w-full md:w-28">
          <Select v-model="filters.method" @update:modelValue="handleSearch">
            <SelectTrigger class="h-9 w-full">
              <SelectValue :placeholder="t('admin.errorLogs.filterMethod')" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all__">{{ t('admin.errorLogs.filterAll') }}</SelectItem>
              <SelectItem v-for="m in methods" :key="m" :value="m">{{ m }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <!-- 路径 -->
        <div class="w-full md:w-52">
          <Input
            v-model="filters.path"
            :placeholder="t('admin.errorLogs.filterPath')"
            @update:modelValue="debouncedSearch"
          />
        </div>
        <!-- 关键词 -->
        <div class="w-full md:w-48">
          <Input
            v-model="filters.keyword"
            :placeholder="t('admin.errorLogs.filterKeyword')"
            @update:modelValue="debouncedSearch"
          />
        </div>
        <!-- IP -->
        <div class="w-full md:w-36">
          <Input
            v-model="filters.clientIp"
            :placeholder="t('admin.errorLogs.filterClientIp')"
            @update:modelValue="debouncedSearch"
          />
        </div>
        <!-- 时间范围 -->
        <div class="flex w-full flex-col gap-2 md:w-auto md:flex-row md:items-center">
          <span class="text-xs text-muted-foreground whitespace-nowrap">{{ t('admin.errorLogs.filterCreatedFrom') }}</span>
          <Input v-model="filters.createdFrom" type="datetime-local" class="h-9 w-full md:w-auto" @update:modelValue="handleSearch" />
          <span class="hidden text-muted-foreground md:inline">-</span>
          <Input v-model="filters.createdTo" type="datetime-local" class="h-9 w-full md:w-auto" @update:modelValue="handleSearch" />
        </div>
        <div class="hidden flex-1 sm:block"></div>
        <Button size="sm" variant="outline" class="w-full sm:w-auto" :disabled="refreshing" @click="refresh">
          {{ t('admin.common.refresh') }}
        </Button>
      </div>
    </div>

    <!-- 表格 -->
    <div class="rounded-xl border border-border bg-card overflow-x-auto">
      <Table class="min-w-[900px]">
        <TableHeader class="border-b border-border bg-muted/40 text-xs uppercase text-muted-foreground">
          <TableRow>
            <TableHead class="px-6 py-3">{{ t('admin.errorLogs.table.id') }}</TableHead>
            <TableHead class="px-4 py-3">{{ t('admin.errorLogs.table.level') }}</TableHead>
            <TableHead class="px-4 py-3">{{ t('admin.errorLogs.table.category') }}</TableHead>
            <TableHead class="min-w-[200px] px-4 py-3">{{ t('admin.errorLogs.table.message') }}</TableHead>
            <TableHead class="min-w-[160px] px-4 py-3">{{ t('admin.errorLogs.table.path') }}</TableHead>
            <TableHead class="px-4 py-3">{{ t('admin.errorLogs.table.statusCode') }}</TableHead>
            <TableHead class="px-4 py-3">{{ t('admin.errorLogs.table.clientIp') }}</TableHead>
            <TableHead class="min-w-[110px] px-4 py-3">{{ t('admin.errorLogs.table.createdAt') }}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody class="divide-y divide-border">
          <TableRow v-if="loading">
            <TableCell :colspan="8" class="p-0">
              <TableSkeleton :columns="8" :rows="5" />
            </TableCell>
          </TableRow>
          <TableRow v-else-if="logs.length === 0">
            <TableCell colspan="8" class="px-6 py-8 text-center text-muted-foreground">
              {{ t('admin.errorLogs.empty') }}
            </TableCell>
          </TableRow>
          <TableRow
            v-for="item in logs"
            :key="item.id"
            class="cursor-pointer hover:bg-muted/30"
            @click="openDetail(item)"
          >
            <TableCell class="px-6 py-4"><IdCell :value="item.id" /></TableCell>
            <TableCell class="px-4 py-4">
              <span class="inline-flex rounded-full border px-2.5 py-1 text-xs" :class="levelClass(item.level)">
                {{ levelLabel(item.level) }}
              </span>
            </TableCell>
            <TableCell class="px-4 py-4 text-xs text-muted-foreground">{{ categoryLabel(item.category) }}</TableCell>
            <TableCell class="min-w-[200px] max-w-[300px] px-4 py-4 text-xs text-foreground">
              <div class="truncate" :title="item.message">{{ item.message }}</div>
            </TableCell>
            <TableCell class="min-w-[160px] px-4 py-4 font-mono text-xs text-muted-foreground">
              <span class="mr-1 text-foreground">{{ item.method }}</span>{{ item.path }}
            </TableCell>
            <TableCell class="px-4 py-4 text-xs font-mono text-foreground">{{ item.status_code }}</TableCell>
            <TableCell class="px-4 py-4 font-mono text-xs text-muted-foreground">{{ item.client_ip || '-' }}</TableCell>
            <TableCell class="min-w-[110px] px-4 py-4 text-xs text-muted-foreground">{{ formatDate(item.created_at) }}</TableCell>
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

    <!-- 详情抽屉（点击行展开） -->
    <Teleport to="body">
      <div v-if="selectedLog" class="fixed inset-0 z-50 flex items-end justify-end" @click.self="selectedLog = null">
        <div class="h-full w-full max-w-2xl overflow-y-auto bg-background shadow-2xl border-l border-border p-6 flex flex-col gap-4">
          <div class="flex items-center justify-between">
            <h2 class="text-lg font-semibold">{{ t('admin.errorLogs.detailTitle') }}</h2>
            <Button size="sm" variant="ghost" @click="selectedLog = null">✕</Button>
          </div>

          <div class="space-y-3 text-sm">
            <div class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">ID</span>
              <span class="col-span-2 font-mono">{{ selectedLog.id }}</span>
            </div>
            <div class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">{{ t('admin.errorLogs.table.createdAt') }}</span>
              <span class="col-span-2">{{ formatDate(selectedLog.created_at) }}</span>
            </div>
            <div class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">{{ t('admin.errorLogs.table.level') }}</span>
              <span class="col-span-2">
                <span class="inline-flex rounded-full border px-2 py-0.5 text-xs" :class="levelClass(selectedLog.level)">
                  {{ levelLabel(selectedLog.level) }}
                </span>
              </span>
            </div>
            <div class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">{{ t('admin.errorLogs.table.category') }}</span>
              <span class="col-span-2">{{ categoryLabel(selectedLog.category) }}</span>
            </div>
            <div class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">{{ t('admin.errorLogs.table.path') }}</span>
              <span class="col-span-2 font-mono break-all">
                <span class="mr-1 font-semibold">{{ selectedLog.method }}</span>{{ selectedLog.path }}
              </span>
            </div>
            <div class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">{{ t('admin.errorLogs.table.statusCode') }}</span>
              <span class="col-span-2 font-mono">{{ selectedLog.status_code }}</span>
            </div>
            <div class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">{{ t('admin.errorLogs.table.requestId') }}</span>
              <span class="col-span-2 font-mono break-all text-xs">{{ selectedLog.request_id || '-' }}</span>
            </div>
            <div class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">{{ t('admin.errorLogs.table.clientIp') }}</span>
              <span class="col-span-2 font-mono">{{ selectedLog.client_ip || '-' }}</span>
            </div>
            <div v-if="selectedLog.user_id" class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">User ID</span>
              <span class="col-span-2 font-mono">{{ selectedLog.user_id }}</span>
            </div>
            <div v-if="selectedLog.admin_id" class="grid grid-cols-3 gap-2">
              <span class="text-muted-foreground">Admin ID</span>
              <span class="col-span-2 font-mono">{{ selectedLog.admin_id }}</span>
            </div>
          </div>

          <!-- 错误消息 -->
          <div class="space-y-1">
            <p class="text-sm font-medium text-muted-foreground">{{ t('admin.errorLogs.table.message') }}</p>
            <pre class="rounded-lg bg-muted p-3 text-xs leading-relaxed whitespace-pre-wrap break-all font-mono">{{ selectedLog.message }}</pre>
          </div>

          <!-- 详情/堆栈 -->
          <div v-if="selectedLog.detail" class="space-y-1">
            <p class="text-sm font-medium text-muted-foreground">Detail</p>
            <pre class="rounded-lg bg-muted p-3 text-xs leading-relaxed whitespace-pre-wrap break-all font-mono max-h-64 overflow-y-auto">{{ selectedLog.detail }}</pre>
          </div>

          <!-- User-Agent -->
          <div v-if="selectedLog.user_agent" class="space-y-1">
            <p class="text-sm font-medium text-muted-foreground">User-Agent</p>
            <p class="rounded-lg bg-muted p-3 text-xs break-all font-mono">{{ selectedLog.user_agent }}</p>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>
