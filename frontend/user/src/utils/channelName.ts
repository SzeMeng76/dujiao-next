/**
 * 支付渠道名称按当前语言取值。
 * 优先读多语言字段 name_i18n（接口新字段），缺失时回落到 name 快照，
 * 最后回落到 channel_type，保证任何情况下都有文案可展示。
 */
export const resolveLocalizedChannelName = (channel: any, locale: string): string => {
  if (!channel) return ''
  const i18n = channel.name_i18n
  if (i18n && typeof i18n === 'object') {
    const candidates = [locale, 'zh-CN', 'en-US']
    for (const key of candidates) {
      const value = i18n[key]
      if (value !== undefined && value !== null && String(value).trim() !== '') {
        return String(value)
      }
    }
  }
  const fallback = String(channel.name || '').trim()
  if (fallback) return fallback
  return String(channel.channel_type || '').trim()
}
