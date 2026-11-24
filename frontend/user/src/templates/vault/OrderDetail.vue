<template>
  <div class="mx-auto w-full max-w-[1180px] px-6 pb-8">
    <BreadcrumbNav
      class="my-3.5 mt-[18px]"
      :items="[
        { label: t('nav.home'), to: '/' },
        { label: t('orders.title'), to: '/me/orders' },
        { label: t('orderDetail.title') },
      ]"
    />

    <header class="mb-[18px]">
      <h1 class="mb-1.5 text-3xl font-extrabold">{{ t('orderDetail.title') }}</h1>
      <p class="text-muted-foreground">{{ t('orderDetail.subtitle') }}</p>
    </header>

    <!-- Loading -->
    <div v-if="loading" class="rounded-xl border bg-card p-[22px]">
      <div class="mb-4 h-5 w-[35%] rounded bg-secondary"></div>
      <div class="h-[200px] rounded-md bg-secondary"></div>
    </div>

    <!-- 不存在 -->
    <EmptyState v-else-if="!order" class="my-6" icon="alert" :title="t('orderDetail.notFound')">
      <template #action>
        <Button class="rounded-full" size="sm" @click="debouncedLoadOrder()">{{ t('errorBoundary.retry') }}</Button>
      </template>
    </EmptyState>

    <template v-else>
      <div class="mb-[18px] flex flex-wrap items-start justify-between gap-[18px] rounded-xl border bg-card p-[22px]">
        <div>
          <div class="text-[11px] uppercase tracking-[0.06em] text-muted-foreground">{{ t('orders.orderNo') }}</div>
          <div class="mt-1 font-bold">{{ order.order_no }}</div>
          <div class="mt-1.5 text-[13px] text-muted-foreground">{{ t('orderDetail.createdAtLabel') }}：{{ formatDate(order.created_at) }}</div>
        </div>
        <div>
          <div class="text-[11px] uppercase tracking-[0.06em] text-muted-foreground">{{ t('orderDetail.amountTotal') }}</div>
          <div class="mt-1 text-2xl font-extrabold tabular-nums">{{ formatMoney(order.total_amount, order.currency) }}</div>
        </div>
        <div class="flex flex-wrap items-center gap-2.5">
          <Badge :variant="statusVariant(order.status)" class="rounded-full">{{ statusLabel(order.status) }}</Badge>
          <Button v-if="order.status === 'pending_payment'" as-child size="sm" class="rounded-full">
            <RouterLink :to="`/pay?order_no=${order.order_no}`">{{ t('orderDetail.payNow') }}</RouterLink>
          </Button>
          <Button v-if="order.status === 'pending_payment'" type="button" variant="outline" size="sm" class="rounded-full border-destructive text-destructive hover:bg-destructive/10" @click="cancelOrder">{{ t('orderDetail.cancel') }}</Button>
        </div>
      </div>

      <VaultOrderBody
        :order="order"
        variant="user"
        :fulfillment-downloading="fulfillmentDownloading"
        @download="handleDownloadFulfillment"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import BreadcrumbNav from '../../components/BreadcrumbNav.vue'
import EmptyState from '../../components/EmptyState.vue'
import VaultOrderBody from './components/VaultOrderBody.vue'
import { useOrderDetail } from '../../composables/useOrderDetail'

const { t } = useI18n()

const {
  loading, order, debouncedLoadOrder, cancelOrder, fulfillmentDownloading, handleDownloadFulfillment,
  statusLabel, statusVariant, formatDate, formatMoney,
} = useOrderDetail()
</script>
