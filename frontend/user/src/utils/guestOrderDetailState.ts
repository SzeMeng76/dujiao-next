export type GuestOrderDetailViewState = 'loading' | 'auth' | 'lookup' | 'detail' | 'empty'

interface GuestOrderDetailViewStateInput {
  loading: boolean
  order: unknown
  showAuthForm: boolean
  lookupCaptchaEnabled?: boolean
}

export const resolveGuestOrderDetailViewState = ({
  loading,
  order,
  showAuthForm,
  lookupCaptchaEnabled = false,
}: GuestOrderDetailViewStateInput): GuestOrderDetailViewState => {
  if (loading) return 'loading'
  if (showAuthForm) return lookupCaptchaEnabled ? 'lookup' : 'auth'
  if (order) return 'detail'
  return 'empty'
}
