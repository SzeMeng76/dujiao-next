import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { guestOrderAPI } from '../api'
import { orderStatusVariant, orderStatusLabel } from '../utils/status'
import { debounceAsync } from '../utils/debounce'
import { amountToCents } from '../utils/money'
import { clearGuestOrderAuth, loadGuestOrderAuth, saveGuestOrderAuth } from '../utils/guestOrderAuth'
import { useAppStore } from '../stores/app'
import TurnstileCaptcha from '../components/captcha/TurnstileCaptcha.vue'

/**
 * 游客订单查询/列表逻辑（classic + vault 共用）。
 */
export function useGuestOrders() {
  const { t } = useI18n()
  const appStore = useAppStore()

  const savedAuth = ref<{ email: string; order_password: string }>({ email: '', order_password: '' })
  const email = ref('')
  const orderPassword = ref('')
  const orderNo = ref('')
  const loading = ref(false)
  const error = ref('')
  const orders = ref<any[]>([])
  const pagination = ref({
    page: 1,
    page_size: 20,
    total: 0,
    total_page: 1,
  })

  const guestTurnstileToken = ref('')
  const guestTurnstileRef = ref<InstanceType<typeof TurnstileCaptcha> | null>(null)
  const captchaConfig = computed(() => appStore.config?.captcha || null)
  const captchaProvider = computed(() => String(captchaConfig.value?.provider || 'none'))
  const lookupCaptchaEnabled = computed(() => !!captchaConfig.value?.scenes?.guest_lookup_order && captchaProvider.value !== 'none')
  const lookupTurnstileSiteKey = computed(() => String(captchaConfig.value?.turnstile?.site_key || ''))
  const isOrderNoOnlyMode = computed(() => Boolean(orderNo.value) && !email.value && !orderPassword.value)

  const loadSavedAuth = () => {
    savedAuth.value = loadGuestOrderAuth()
    email.value = savedAuth.value.email
    orderPassword.value = savedAuth.value.order_password
  }

  const hasSavedAuth = computed(() => Boolean(savedAuth.value.email || savedAuth.value.order_password))

  const persistAuth = () => {
    const payload = {
      email: email.value,
      order_password: orderPassword.value,
    }
    saveGuestOrderAuth(payload)
    savedAuth.value = payload
  }

  const clearSaved = () => {
    clearGuestOrderAuth()
    savedAuth.value = { email: '', order_password: '' }
    email.value = ''
    orderPassword.value = ''
    orderNo.value = ''
    orders.value = []
    pagination.value = {
      page: 1,
      page_size: 20,
      total: 0,
      total_page: 1,
    }
    error.value = ''
  }

  const resetGuestTurnstile = () => {
    guestTurnstileRef.value?.reset()
    guestTurnstileToken.value = ''
  }

  const handleSearch = async () => {
    error.value = ''
    const hasOrderNo = Boolean(orderNo.value)
    const hasCredentials = Boolean(email.value && orderPassword.value)

    if (!hasOrderNo && !hasCredentials) {
      error.value = t('guestOrders.errors.missing')
      return
    }

    if (hasCredentials) {
      persistAuth()
      await debouncedLoadOrders(1)
      return
    }

    if (lookupCaptchaEnabled.value && !guestTurnstileToken.value) {
      error.value = t('guestOrders.errors.captchaRequired')
      return
    }
    await debouncedLoadOrderByNoOnly()
  }

  const loadOrders = async (page: number) => {
    loading.value = true
    try {
      const response = await guestOrderAPI.list({
        email: email.value,
        order_password: orderPassword.value,
        order_no: orderNo.value || undefined,
        page,
        page_size: pagination.value.page_size,
      })
      orders.value = response.data.data || []
      pagination.value = response.data.pagination || pagination.value
      if (orderNo.value && orders.value.length === 0) {
        error.value = t('guestOrders.errors.notFound')
      }
    } catch (err: any) {
      orders.value = []
      error.value = err.message || t('guestOrders.errors.searchFailed')
    } finally {
      loading.value = false
    }
  }

  const loadOrderByNoOnly = async () => {
    loading.value = true
    try {
      const response = await guestOrderAPI.lookupByOrderNo(orderNo.value, {
        turnstile_token: guestTurnstileToken.value || undefined,
      })
      orders.value = response.data.data ? [response.data.data] : []
      pagination.value = { page: 1, page_size: 1, total: orders.value.length, total_page: 1 }
      if (orders.value.length === 0) {
        error.value = t('guestOrders.errors.notFound')
      }
    } catch (err: any) {
      orders.value = []
      error.value = err.message || t('guestOrders.errors.notFound')
      resetGuestTurnstile()
    } finally {
      loading.value = false
    }
  }

  const debouncedLoadOrders = debounceAsync(loadOrders, 300)
  const debouncedLoadOrderByNoOnly = debounceAsync(loadOrderByNoOnly, 300)

  const emptyMessage = computed(() => {
    if (orderNo.value) {
      return t('guestOrders.emptyOrderNo')
    }
    return t('guestOrders.empty')
  })

  const changePage = (page: number) => {
    if (page < 1 || page > pagination.value.total_page) return
    debouncedLoadOrders(page)
  }

  const statusLabel = (status: string) => orderStatusLabel(t, status)
  const statusVariant = (status: string) => orderStatusVariant(status)

  // vault 模板：BadgeTone → vault pill 类名（classic 不使用）
  const statusPillClass = (status?: string) => {
    const map: Record<string, string> = {
      success: 'pill-done',
      warning: 'pill-low',
      danger: 'pill-sale',
      info: 'pill-stock',
      accent: 'pill-sale',
      neutral: 'pill-out',
    }
    return map[orderStatusVariant(status)] || 'pill-out'
  }

  const formatMoney = (amount?: string, currency?: string) => {
    if (amount === null || amount === undefined || amount === '') return '-'
    if (currency === null || currency === undefined || currency === '') {
      return String(amount)
    }
    return `${amount} ${currency}`
  }

  const formatDiscountMoney = (amount?: string, currency?: string) => {
    return hasDiscountAmount(amount) ? `-${formatMoney(amount, currency)}` : formatMoney(amount, currency)
  }

  const hasDiscountAmount = (amount?: string) => {
    if (amount === null || amount === undefined || amount === '') return false
    const valueCents = amountToCents(amount)
    return valueCents !== null && valueCents > 0
  }

  const hasDiscount = (order: any) => {
    if (!order) return false
    return hasDiscountAmount(order.discount_amount) || hasDiscountAmount(order.promotion_discount_amount)
  }

  const formatDate = (raw?: string) => {
    if (!raw) return ''
    const date = new Date(raw)
    if (Number.isNaN(date.getTime())) return raw
    return date.toLocaleString()
  }

  onMounted(() => {
    loadSavedAuth()
    if (hasSavedAuth.value) {
      debouncedLoadOrders(1)
    }
  })

  onUnmounted(() => {
    debouncedLoadOrders.cancel()
    debouncedLoadOrderByNoOnly.cancel()
  })

  return {
    savedAuth,
    email,
    orderPassword,
    orderNo,
    loading,
    error,
    orders,
    pagination,
    hasSavedAuth,
    clearSaved,
    handleSearch,
    emptyMessage,
    changePage,
    statusLabel,
    statusVariant,
    statusPillClass,
    formatMoney,
    formatDiscountMoney,
    hasDiscountAmount,
    hasDiscount,
    formatDate,
    isOrderNoOnlyMode,
    lookupCaptchaEnabled,
    lookupTurnstileSiteKey,
    guestTurnstileToken,
    guestTurnstileRef,
  }
}
