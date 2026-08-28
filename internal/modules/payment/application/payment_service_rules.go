package application

import (
	"fmt"
	"strings"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	orderapp "github.com/dujiao-next/internal/modules/order/application"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"github.com/dujiao-next/internal/constants"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"

	"github.com/shopspring/decimal"
)

// calculatePaymentAmounts 按创建时配置计算用户实付金额、渠道手续费及不可变策略快照。
func calculatePaymentAmounts(baseAmount, feeRate, fixedFee decimal.Decimal, customerFeeEnabled bool) (paymentAmount, feeAmount decimal.Decimal, feePolicy string) {
	baseAmount = baseAmount.Round(2)
	feeAmount = fixedFee.Round(2)
	if feeRate.GreaterThan(decimal.Zero) {
		feeAmount = feeAmount.Add(baseAmount.Mul(feeRate).Div(decimal.NewFromInt(100))).Round(2)
	}
	if feeAmount.IsZero() {
		return baseAmount, feeAmount, constants.PaymentFeePolicyNone
	}
	if customerFeeEnabled {
		return baseAmount.Add(feeAmount).Round(2), feeAmount, constants.PaymentFeePolicyCustomerSurcharge
	}
	return baseAmount, feeAmount, constants.PaymentFeePolicyMerchantAbsorbed
}

// paymentCoveredOrderAmount 推算一笔支付创建时承诺覆盖的订单在线应付额（订单币种口径）。
//
// Amount / FeeAmount / FeePolicy 均为创建时快照：用户承担手续费的策略下 Amount
// 含加收部分，必须扣除后才是订单侧的抵扣基数；商家承担或无手续费时 Amount 即基数。
// 升级前未写入策略快照（FeePolicy 为空）且带正数手续费的历史记录，与 CreatePayment
// 的 legacy 判定保持一致，按用户承担解释。
//
// Binance Pay 等按汇率换算结算币种的网关（见各 adapter CreatePayment 里
// cfg.NeedsCurrencyConversion 分支），创建支付后 payment.Amount/Currency 会被
// applyProviderPayment 改写为网关结算币种（如 USDT），不再是订单币种下的金额；
// 网关 webhook 回调回来的金额同样是结算币种（Binance 回调本身就是 USDT 数值，
// 无法要求网关改成 CNY），若直接拿这个结算币种数值与订单 CNY 总额比较，两个不同
// 币种的数字硬比必然判定为金额不足。换算发生时，换算前的订单币种金额已经被
// adapter 写入 payment.ProviderPayload["original_amount"]，此处应优先取用它。
func paymentCoveredOrderAmount(payment *paymentdomain.Payment) decimal.Decimal {
	if payment == nil {
		return decimal.Zero
	}
	covered := payment.Amount.Decimal
	if payment.ProviderPayload != nil {
		if raw, ok := payment.ProviderPayload["original_amount"]; ok {
			if s := strings.TrimSpace(fmt.Sprint(raw)); s != "" {
				if original, err := decimal.NewFromString(s); err == nil {
					covered = original
				}
			}
		}
	}
	fee := payment.FeeAmount.Decimal
	switch payment.FeePolicy {
	case constants.PaymentFeePolicyCustomerSurcharge, constants.PaymentFeePolicyLegacyCustomerSurcharge:
		covered = covered.Sub(fee)
	case "":
		if fee.IsPositive() {
			covered = covered.Sub(fee)
		}
	}
	return normalizeOrderAmount(covered)
}

// normalizeOrderAmount 归一化金额精度与下限。
func normalizeOrderAmount(amount decimal.Decimal) decimal.Decimal {
	normalized := amount.Round(2)
	if normalized.LessThan(decimal.Zero) {
		return decimal.Zero
	}
	return normalized
}

func pickFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func shouldMarkFulfilling(order *orderdomain.Order) bool {
	if order == nil {
		return false
	}
	if len(order.Items) == 0 {
		return false
	}
	for _, item := range order.Items {
		fulfillmentType := strings.TrimSpace(item.FulfillmentType)
		if fulfillmentType == "" || fulfillmentType == constants.FulfillmentTypeManual || fulfillmentType == constants.FulfillmentTypeUpstream {
			return true
		}
	}
	return false
}

func shouldUseCNYPaymentCurrency(channel *paymentdomain.PaymentChannel) bool {
	if channel == nil {
		return false
	}
	providerType := strings.ToLower(strings.TrimSpace(channel.ProviderType))
	if providerType != constants.PaymentProviderOfficial {
		return false
	}
	channelType := strings.ToLower(strings.TrimSpace(channel.ChannelType))
	return channelType == constants.PaymentChannelTypeWechat || channelType == constants.PaymentChannelTypeAlipay
}

func validatePaymentAmountForChannel(amount decimal.Decimal, channel *paymentdomain.PaymentChannel) error {
	if channel == nil {
		return nil
	}
	amountOverflow20_2 := decimal.NewFromInt(1000000000000000000)
	if amount.GreaterThanOrEqual(amountOverflow20_2) {
		return ErrPaymentAmountTooLarge
	}
	minAmount := channel.MinAmount.Decimal
	maxAmount := channel.MaxAmount.Decimal
	if minAmount.GreaterThan(decimal.Zero) && amount.LessThan(minAmount) {
		return ErrPaymentAmountTooSmall
	}
	if maxAmount.GreaterThan(decimal.Zero) && amount.GreaterThan(maxAmount) {
		return ErrPaymentAmountTooLarge
	}
	return nil
}

func validatePaymentCurrencyForChannel(currency string, channel *paymentdomain.PaymentChannel) error {
	normalized := strings.ToUpper(strings.TrimSpace(currency))
	if !settingsapp.IsCurrencyCode(normalized) {
		return ErrPaymentCurrencyMismatch
	}
	if shouldUseCNYPaymentCurrency(channel) && normalized != constants.SiteCurrencyDefault {
		return ErrPaymentCurrencyMismatch
	}
	return nil
}

func (s *PaymentService) resolveExpireMinutes() int {
	return orderapp.ResolvePaymentExpireMinutes(s.settingService, s.expireMinutes)
}

func normalizePaymentStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func isPaymentStatusValid(status string) bool {
	switch status {
	case constants.PaymentStatusInitiated, constants.PaymentStatusPending, constants.PaymentStatusSuccess, constants.PaymentStatusFailed, constants.PaymentStatusExpired:
		return true
	default:
		return false
	}
}

func shouldAutoFulfill(order *orderdomain.Order) bool {
	if order == nil || len(order.Items) == 0 {
		return false
	}
	for _, item := range order.Items {
		if strings.TrimSpace(item.FulfillmentType) != constants.FulfillmentTypeAuto {
			return false
		}
	}
	return true
}

// isOrderFullyAutoFulfill 判断订单是否完全为自动交付。
// 父订单：所有子订单均满足 shouldAutoFulfill；单订单：自身满足 shouldAutoFulfill。
// 用于支付成功时跳过"已支付"邮件——自动交付会紧接着发送含卡密内容的"已完成"邮件，避免重复打扰。
func isOrderFullyAutoFulfill(order *orderdomain.Order) bool {
	if order == nil {
		return false
	}
	if len(order.Children) > 0 {
		for i := range order.Children {
			if !shouldAutoFulfill(&order.Children[i]) {
				return false
			}
		}
		return true
	}
	return shouldAutoFulfill(order)
}

func buildOrderSubject(order *orderdomain.Order) string {
	if order == nil {
		return ""
	}
	return strings.TrimSpace(order.OrderNo)
}
