package application

import (
	"context"
	"errors"
	"strings"
	"time"

	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"go.uber.org/zap"
)

// WebhookCallbackInput Webhook 回调输入。
type WebhookCallbackInput struct {
	ChannelID uint
	Headers   map[string]string
	Body      []byte
	Context   context.Context
}

// HandleSyncCallback 处理同步 form callback（alipay/epay/epusdt/bepusdt/tokenpay/okpay）。
// 通过 Registry 找到 adapter 的 CallbackVerifier 能力解析并验签 form/body，然后调 HandleCallback。
// channel 必须由 caller 加载好传入（handler 负责找到 payment→channel 并验证类型）。
func (s *PaymentService) HandleSyncCallback(
	channel *paymentdomain.PaymentChannel,
	form map[string][]string,
	body []byte,
) (*paymentdomain.Payment, error) {
	if channel == nil {
		return nil, ErrPaymentChannelNotFound
	}
	if s.paymentProviderRegistry == nil {
		return nil, ErrPaymentProviderNotSupported
	}

	p, ok := s.paymentProviderRegistry.Lookup(channel.ProviderType, channel.ChannelType)
	if !ok {
		return nil, ErrPaymentProviderNotSupported
	}
	verifier, ok := p.(paymentcontract.GatewayCallbackVerifier)
	if !ok {
		return nil, ErrPaymentProviderNotSupported
	}

	result, err := verifier.VerifyCallback(channel.ConfigJSON, form, body)
	if err != nil {
		return nil, mapProviderErrorToService(err)
	}

	// WebhookResult = CallbackResult（类型别名），findWebhookPayment 直接复用。
	payment, err := s.findWebhookPayment(channel.ID, result)
	if err != nil {
		return nil, err
	}

	// 部分网关（目前是 Globepay）的签名只覆盖鉴权参数，不覆盖订单号与金额，
	// 而创建支付时返回给买家的付款链接必然携带同一组签名，等于把合法签名交给了买家。
	// 这类网关必须以官方订单状态回查结果为准，回调内容只能用来定位订单。
	if confirmer, ok := p.(paymentcontract.GatewayCallbackConfirmer); ok && confirmer.RequireCallbackQuery() {
		confirmed, confirmErr := s.confirmCallbackWithGatewayQuery(confirmer, channel, payment, result)
		if confirmErr != nil {
			return nil, confirmErr
		}
		result = confirmed
	}

	payload := jsonmap.JSON{}
	if result.Payload != nil {
		payload = result.Payload
	}

	callbackInput := PaymentCallbackInput{
		PaymentID:   payment.ID,
		OrderNo:     result.OrderNo,
		ChannelID:   channel.ID,
		Status:      result.Status,
		ProviderRef: pickFirstNonEmpty(result.ProviderRef, payment.ProviderRef),
		Amount:      result.Amount,
		Currency:    strings.ToUpper(strings.TrimSpace(result.Currency)),
		PaidAt:      result.PaidAt,
		Payload:     payload,
	}
	return s.HandleCallback(callbackInput)
}

// confirmCallbackWithGatewayQuery 用官方订单状态回查结果替换回调自述的事实。
//
// 适用于签名不覆盖业务字段的网关（见 paymentcontract.GatewayCallbackConfirmer）：
// 回调正文只用于定位订单，到账与否、金额、币种、支付时间一律以官方查询为准。
// 查询失败或查无此单时返回错误，绝不按"未支付"或"已支付"放行。
// gatewayConfirmPendingRetryDelays 是回查遇到"网关仍显示待支付"时的重试间隔。
//
// 回调先于网关订单状态落定到达是正常现象（网关异步更新状态），若立刻判定为未支付
// 会把真实付款卡在待支付。这里做短暂轮询；总等待约 3.5s，远小于网关自身的重试周期，
// 不会拖垮回调接口。轮询完仍是待支付时，返回 ErrPaymentGatewayResponseInvalid，
// 让网关按其重试策略重新投递（文档说明最多重试 3 次）。
var gatewayConfirmPendingRetryDelays = []time.Duration{
	400 * time.Millisecond,
	600 * time.Millisecond,
	1000 * time.Millisecond,
	1500 * time.Millisecond,
}

// queryGatewayWithPendingRetry 回查官方订单状态，遇到待支付时短暂重试。
// 查询本身失败（网络错误、签名错误、订单不存在）不重试，直接返回。
func (s *PaymentService) queryGatewayWithPendingRetry(
	confirmer paymentcontract.GatewayCallbackConfirmer,
	channel *paymentdomain.PaymentChannel,
	queryRef string,
	log *zap.SugaredLogger,
) (*paymentcontract.GatewayQueryResult, error) {
	var lastResult *paymentcontract.GatewayQueryResult

	attempts := len(gatewayConfirmPendingRetryDelays) + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			time.Sleep(gatewayConfirmPendingRetryDelays[attempt-1])
		}

		ctx, cancel := detachOutboundRequestContext(context.Background())
		queried, err := confirmer.QueryPayment(ctx, channel.ConfigJSON, queryRef)
		cancel()

		if err != nil {
			// 回查失败一律拒绝落库：宁可让订单停留在待支付，也不能凭一份未证实的回调履约。
			log.Errorw("payment_callback_confirm_query_failed",
				"query_ref", queryRef, "attempt", attempt+1, "error", err)
			return nil, mapProviderErrorToService(err)
		}
		if queried == nil {
			log.Warnw("payment_callback_confirm_query_empty", "query_ref", queryRef)
			return nil, ErrPaymentGatewayResponseInvalid
		}

		lastResult = queried
		status := strings.TrimSpace(queried.Status)
		if status != "" && status != constants.PaymentStatusPending {
			return queried, nil
		}

		log.Infow("payment_callback_confirm_query_pending",
			"query_ref", queryRef, "attempt", attempt+1, "max_attempts", attempts)
	}

	// 轮询结束仍是待支付：返回错误让网关重试投递，绝不当作成功履约。
	log.Warnw("payment_callback_confirm_query_still_pending",
		"query_ref", queryRef, "attempts", attempts,
		"query_status", lastResult.Status)
	return nil, ErrPaymentGatewayResponseInvalid
}

func (s *PaymentService) confirmCallbackWithGatewayQuery(
	confirmer paymentcontract.GatewayCallbackConfirmer,
	channel *paymentdomain.PaymentChannel,
	payment *paymentdomain.Payment,
	callback *paymentcontract.GatewayCallbackResult,
) (*paymentcontract.GatewayCallbackResult, error) {
	log := paymentLogger(
		"provider_type", channel.ProviderType,
		"channel_id", channel.ID,
		"payment_id", payment.ID,
		"callback_order_no", strings.TrimSpace(callback.OrderNo),
	)

	// 回查必须使用创建时实际提交给网关的商户订单号（partner_order_id），
	// 而不是回调自述的单号，也不是网关流水号。
	queryRef := strings.TrimSpace(payment.GatewayOrderNo)
	if queryRef == "" {
		queryRef = strings.TrimSpace(payment.ProviderRef)
	}
	if queryRef == "" {
		log.Warnw("payment_callback_confirm_missing_query_ref")
		return nil, ErrPaymentInvalid
	}

	queried, err := s.queryGatewayWithPendingRetry(confirmer, channel, queryRef, log)
	if err != nil {
		return nil, err
	}

	status := strings.TrimSpace(queried.Status)
	if status == "" {
		status = constants.PaymentStatusPending
	}
	log.Infow("payment_callback_confirmed_by_query",
		"query_ref", queryRef,
		"query_status", status,
		"query_amount", queried.Amount.String(),
		"query_currency", queried.Currency,
	)

	payload := callback.Payload
	if payload == nil {
		payload = jsonmap.JSON{}
	}
	// 保留回调原文供取证，同时标明"该回调已由官方查询确认"，避免事后误判。
	payload["_dujiao_next_confirmed_by_query"] = true
	payload["_dujiao_next_query_status"] = status
	if queried.Payload != nil {
		payload["_dujiao_next_query_payload"] = queried.Payload
	}

	return &paymentcontract.GatewayCallbackResult{
		OrderNo:     callback.OrderNo,
		ProviderRef: pickFirstNonEmpty(queried.ProviderRef, callback.ProviderRef),
		Status:      status,
		Amount:      queried.Amount,
		Currency:    strings.ToUpper(strings.TrimSpace(queried.Currency)),
		PaidAt:      queried.PaidAt,
		Payload:     payload,
	}, nil
}

// HandlePaypalWebhook 处理 PayPal webhook。
func (s *PaymentService) HandlePaypalWebhook(input WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	return s.handleWebhookViaRegistry(
		input,
		constants.PaymentProviderOfficial,
		constants.PaymentChannelTypePaypal,
	)
}

// HandleDujiaoPayWebhook 处理 DujiaoPay webhook。
//
// DujiaoPay 的 channel_type 是 token_id（tron-usdt/base-usdc 等），同一个 webhook
// 入口不能预先知道 token_id。channel_id 缺失时按 provider_type 拉取所有启用渠道，
// 用 webhook_secret 逐个验签；只有签名匹配的渠道才会进入落库流程。
func (s *PaymentService) HandleDujiaoPayWebhook(input WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	log := paymentLogger(
		"provider", constants.PaymentProviderDujiaoPay,
		"channel_id", input.ChannelID,
		"body_size", len(input.Body),
	)

	if input.ChannelID == 0 {
		candidates, _, err := s.channelRepo.List(paymentcontract.ChannelListFilter{
			ProviderType: constants.PaymentProviderDujiaoPay,
			ActiveOnly:   true,
		})
		if err != nil {
			log.Errorw("payment_webhook_candidates_list_failed", "error", err)
			return nil, "", ErrPaymentUpdateFailed
		}
		if len(candidates) == 0 {
			log.Warnw("payment_webhook_no_candidate_channel")
			return nil, "", ErrPaymentChannelNotFound
		}

		var lastErr error
		for i := range candidates {
			channel := candidates[i]
			result, err := s.tryParseWebhookWithChannel(&channel, input)
			if err != nil {
				log.Debugw("payment_webhook_candidate_parse_failed",
					"candidate_channel_id", channel.ID,
					"channel_type", channel.ChannelType,
					"error", err,
				)
				lastErr = err
				continue
			}
			log.Infow("payment_webhook_candidate_matched", "candidate_channel_id", channel.ID, "channel_type", channel.ChannelType)
			return s.commitVerifiedWebhook(&channel, result, log)
		}
		if lastErr == nil {
			lastErr = ErrPaymentProviderNotSupported
		}
		log.Warnw("payment_webhook_all_candidates_failed", "candidate_count", len(candidates), "last_error", lastErr)
		return nil, "", lastErr
	}

	channel, err := s.channelRepo.GetByID(input.ChannelID)
	if err != nil {
		log.Errorw("payment_webhook_channel_fetch_failed", "error", err)
		return nil, "", ErrPaymentUpdateFailed
	}
	if channel == nil {
		log.Warnw("payment_webhook_channel_not_found")
		return nil, "", ErrPaymentChannelNotFound
	}
	if strings.ToLower(strings.TrimSpace(channel.ProviderType)) != constants.PaymentProviderDujiaoPay {
		log.Warnw("payment_webhook_provider_mismatch",
			"provider_type", channel.ProviderType,
			"channel_type", channel.ChannelType,
		)
		return nil, "", ErrPaymentProviderNotSupported
	}

	result, err := s.tryParseWebhookWithChannel(channel, input)
	if err != nil {
		log.Warnw("payment_webhook_parse_failed", "error", err)
		return nil, "", err
	}
	return s.commitVerifiedWebhook(channel, result, log)
}

// HandleHashpayWebhook 处理 HashPay webhook。
//
// HashPay 回调是用商户公钥加密的 RSA-OAEP+AES-GCM 信封，没有 channel_id 时按
// provider_type 拉取所有启用渠道逐个用私钥试解密；错配的私钥在 OAEP/GCM 阶段
// 必然失败，只有解密成功的渠道才会进入落库流程（同 DujiaoPay 的盲匹配语义）。
func (s *PaymentService) HandleHashpayWebhook(input WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	log := paymentLogger(
		"provider", constants.PaymentProviderHashpay,
		"channel_id", input.ChannelID,
		"body_size", len(input.Body),
	)

	if input.ChannelID == 0 {
		candidates, _, err := s.channelRepo.List(paymentcontract.ChannelListFilter{
			ProviderType: constants.PaymentProviderHashpay,
			ActiveOnly:   true,
		})
		if err != nil {
			log.Errorw("payment_webhook_candidates_list_failed", "error", err)
			return nil, "", ErrPaymentUpdateFailed
		}
		if len(candidates) == 0 {
			log.Warnw("payment_webhook_no_candidate_channel")
			return nil, "", ErrPaymentChannelNotFound
		}

		var lastErr error
		for i := range candidates {
			channel := candidates[i]
			result, err := s.tryParseWebhookWithChannel(&channel, input)
			if err != nil {
				log.Debugw("payment_webhook_candidate_parse_failed",
					"candidate_channel_id", channel.ID,
					"error", err,
				)
				lastErr = err
				continue
			}
			log.Infow("payment_webhook_candidate_matched", "candidate_channel_id", channel.ID)
			return s.commitVerifiedWebhook(&channel, result, log)
		}
		if lastErr == nil {
			lastErr = ErrPaymentProviderNotSupported
		}
		log.Warnw("payment_webhook_all_candidates_failed", "candidate_count", len(candidates), "last_error", lastErr)
		return nil, "", lastErr
	}

	channel, err := s.channelRepo.GetByID(input.ChannelID)
	if err != nil {
		log.Errorw("payment_webhook_channel_fetch_failed", "error", err)
		return nil, "", ErrPaymentUpdateFailed
	}
	if channel == nil {
		log.Warnw("payment_webhook_channel_not_found")
		return nil, "", ErrPaymentChannelNotFound
	}
	if strings.ToLower(strings.TrimSpace(channel.ProviderType)) != constants.PaymentProviderHashpay {
		log.Warnw("payment_webhook_provider_mismatch",
			"provider_type", channel.ProviderType,
			"channel_type", channel.ChannelType,
		)
		return nil, "", ErrPaymentProviderNotSupported
	}

	result, err := s.tryParseWebhookWithChannel(channel, input)
	if err != nil {
		log.Warnw("payment_webhook_parse_failed", "error", err)
		return nil, "", err
	}
	return s.commitVerifiedWebhook(channel, result, log)
}

// HandleCryptomusWebhook 处理 Cryptomus webhook。
//
// Cryptomus 回调是 body 内嵌 sign 字段的 MD5 签名（无独立请求头），没有 channel_id 时按
// provider_type 拉取所有启用渠道逐个用 payment_api_key 试验签；只有验签成功的渠道才会
// 进入落库流程（同 DujiaoPay/HashPay 的盲匹配语义）。
func (s *PaymentService) HandleCryptomusWebhook(input WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	log := paymentLogger(
		"provider", constants.PaymentProviderCryptomus,
		"channel_id", input.ChannelID,
		"body_size", len(input.Body),
	)

	if input.ChannelID == 0 {
		candidates, _, err := s.channelRepo.List(paymentcontract.ChannelListFilter{
			ProviderType: constants.PaymentProviderCryptomus,
			ActiveOnly:   true,
		})
		if err != nil {
			log.Errorw("payment_webhook_candidates_list_failed", "error", err)
			return nil, "", ErrPaymentUpdateFailed
		}
		if len(candidates) == 0 {
			log.Warnw("payment_webhook_no_candidate_channel")
			return nil, "", ErrPaymentChannelNotFound
		}

		var lastErr error
		for i := range candidates {
			channel := candidates[i]
			result, err := s.tryParseWebhookWithChannel(&channel, input)
			if err != nil {
				log.Debugw("payment_webhook_candidate_parse_failed",
					"candidate_channel_id", channel.ID,
					"error", err,
				)
				lastErr = err
				continue
			}
			log.Infow("payment_webhook_candidate_matched", "candidate_channel_id", channel.ID)
			return s.commitVerifiedWebhook(&channel, result, log)
		}
		if lastErr == nil {
			lastErr = ErrPaymentProviderNotSupported
		}
		log.Warnw("payment_webhook_all_candidates_failed", "candidate_count", len(candidates), "last_error", lastErr)
		return nil, "", lastErr
	}

	channel, err := s.channelRepo.GetByID(input.ChannelID)
	if err != nil {
		log.Errorw("payment_webhook_channel_fetch_failed", "error", err)
		return nil, "", ErrPaymentUpdateFailed
	}
	if channel == nil {
		log.Warnw("payment_webhook_channel_not_found")
		return nil, "", ErrPaymentChannelNotFound
	}
	if strings.ToLower(strings.TrimSpace(channel.ProviderType)) != constants.PaymentProviderCryptomus {
		log.Warnw("payment_webhook_provider_mismatch",
			"provider_type", channel.ProviderType,
			"channel_type", channel.ChannelType,
		)
		return nil, "", ErrPaymentProviderNotSupported
	}

	result, err := s.tryParseWebhookWithChannel(channel, input)
	if err != nil {
		log.Warnw("payment_webhook_parse_failed", "error", err)
		return nil, "", err
	}
	return s.commitVerifiedWebhook(channel, result, log)
}

// handleWebhookViaRegistry 通过 Registry 路由 webhook 解析。
//
// channel_id 在 URL query 缺失时:
//   - 微信不允许 notify_url 携带 query 参数(微信官方 V3 规范),wechat webhook
//     必须支持遍历候选 channel,逐个用 api_v3_key 尝试 AES-GCM 解密验签;
//   - stripe 的 endpoint_secret 也具备同等的"试错验签"能力,顺带支持;
//   - paypal 的 webhook_id 校验缺乏自识别机制,必须强制 channel_id。
func (s *PaymentService) handleWebhookViaRegistry(
	input WebhookCallbackInput,
	expectedProviderType string,
	expectedChannelType string,
) (*paymentdomain.Payment, string, error) {
	log := paymentLogger(
		"provider", expectedChannelType,
		"channel_id", input.ChannelID,
		"body_size", len(input.Body),
	)

	if input.ChannelID == 0 {
		if !supportsBlindWebhookCandidateMatching(expectedChannelType) {
			log.Warnw("payment_webhook_invalid_channel_id")
			return nil, "", ErrPaymentInvalid
		}
		return s.handleWebhookByCandidateIteration(input, expectedProviderType, expectedChannelType, log)
	}

	channel, err := s.channelRepo.GetByID(input.ChannelID)
	if err != nil {
		log.Errorw("payment_webhook_channel_fetch_failed", "error", err)
		return nil, "", ErrPaymentUpdateFailed
	}
	if channel == nil {
		log.Warnw("payment_webhook_channel_not_found")
		return nil, "", ErrPaymentChannelNotFound
	}

	providerType := strings.ToLower(strings.TrimSpace(channel.ProviderType))
	channelType := strings.ToLower(strings.TrimSpace(channel.ChannelType))
	if providerType != expectedProviderType || channelType != expectedChannelType {
		log.Warnw("payment_webhook_provider_mismatch",
			"provider_type", channel.ProviderType,
			"channel_type", channel.ChannelType,
		)
		return nil, "", ErrPaymentProviderNotSupported
	}

	result, err := s.tryParseWebhookWithChannel(channel, input)
	if err != nil {
		log.Warnw("payment_webhook_parse_failed", "error", err)
		return nil, "", err
	}
	return s.commitVerifiedWebhook(channel, result, log)
}

// handleWebhookByCandidateIteration 在 channel_id 缺失时,按 expectedProvider+expectedChannel
// 拉取所有 active channel,挨个尝试解密验签;首个 ParseWebhook 成功的渠道即为目标渠道。
func (s *PaymentService) handleWebhookByCandidateIteration(
	input WebhookCallbackInput,
	expectedProviderType string,
	expectedChannelType string,
	log *zap.SugaredLogger,
) (*paymentdomain.Payment, string, error) {
	candidates, _, err := s.channelRepo.List(paymentcontract.ChannelListFilter{
		ProviderType: expectedProviderType,
		ChannelType:  expectedChannelType,
		ActiveOnly:   true,
	})
	if err != nil {
		log.Errorw("payment_webhook_candidates_list_failed", "error", err)
		return nil, "", ErrPaymentUpdateFailed
	}
	if len(candidates) == 0 {
		log.Warnw("payment_webhook_no_candidate_channel")
		return nil, "", ErrPaymentChannelNotFound
	}

	var lastErr error
	for i := range candidates {
		channel := candidates[i]
		result, err := s.tryParseWebhookWithChannel(&channel, input)
		if err != nil {
			log.Debugw("payment_webhook_candidate_parse_failed",
				"candidate_channel_id", channel.ID,
				"error", err,
			)
			lastErr = err
			continue
		}
		log.Infow("payment_webhook_candidate_matched", "candidate_channel_id", channel.ID)
		return s.commitVerifiedWebhook(&channel, result, log)
	}
	if lastErr == nil {
		lastErr = ErrPaymentProviderNotSupported
	}
	log.Warnw("payment_webhook_all_candidates_failed", "candidate_count", len(candidates), "last_error", lastErr)
	return nil, "", lastErr
}

// tryParseWebhookWithChannel 用指定 channel 的 config 尝试解析 webhook。
// 返回 error 表示该 channel 不匹配(签名/密钥校验失败或 capability 缺失),
// 由 caller 决定 retry 下一个候选还是终止。
func (s *PaymentService) tryParseWebhookWithChannel(
	channel *paymentdomain.PaymentChannel,
	input WebhookCallbackInput,
) (*paymentcontract.GatewayCallbackResult, error) {
	if s.paymentProviderRegistry == nil {
		return nil, ErrPaymentProviderNotSupported
	}
	p, ok := s.paymentProviderRegistry.Lookup(channel.ProviderType, channel.ChannelType)
	if !ok {
		return nil, ErrPaymentProviderNotSupported
	}
	webhooker, ok := p.(paymentcontract.GatewayWebhooker)
	if !ok {
		return nil, ErrPaymentProviderNotSupported
	}

	ctx, cancel := detachOutboundRequestContext(input.Context)
	defer cancel()

	result, err := webhooker.ParseWebhook(ctx, channel.ConfigJSON, input.Headers, input.Body, time.Now())
	if err != nil {
		return nil, mapProviderErrorToService(err)
	}
	return result, nil
}

// commitVerifiedWebhook 在 ParseWebhook 验签通过(已确认 channel 归属)后,
// 反查 payment 并落库。任何错误都是真实的业务/DB 错误,不再 retry 其他 channel。
func (s *PaymentService) commitVerifiedWebhook(
	channel *paymentdomain.PaymentChannel,
	result *paymentcontract.GatewayCallbackResult,
	log *zap.SugaredLogger,
) (*paymentdomain.Payment, string, error) {
	log.Infow("payment_webhook_parsed",
		"channel_id", channel.ID,
		"order_no", result.OrderNo,
		"provider_ref", result.ProviderRef,
		"status", result.Status,
	)

	// status 为空表示 adapter 判断该事件无需处理（不可识别的事件类型），直接忽略。
	if result.Status == "" {
		log.Infow("payment_webhook_status_ignored",
			"channel_id", channel.ID,
			"order_no", result.OrderNo,
			"provider_ref", result.ProviderRef,
		)
		return nil, "", nil
	}

	payment, err := s.findWebhookPayment(channel.ID, result)
	if err != nil {
		if errors.Is(err, ErrPaymentNotFound) {
			log.Infow("payment_webhook_payment_not_found",
				"channel_id", channel.ID,
				"order_no", result.OrderNo,
				"provider_ref", result.ProviderRef,
				"status", result.Status,
			)
			return nil, result.Status, nil
		}
		log.Warnw("payment_webhook_payment_lookup_failed",
			"channel_id", channel.ID,
			"order_no", result.OrderNo,
			"provider_ref", result.ProviderRef,
			"status", result.Status,
			"error", err,
		)
		return nil, result.Status, err
	}

	payload := jsonmap.JSON{}
	if result.Payload != nil {
		payload = result.Payload
	}
	verifiedLegacyCurrency := ""
	if channel.ProviderType == constants.PaymentProviderDujiaoPay &&
		result.Status == constants.PaymentStatusSuccess &&
		payment.Status != constants.PaymentStatusSuccess &&
		!strings.EqualFold(strings.TrimSpace(payment.Currency), strings.TrimSpace(result.Currency)) {
		if _, hasSnapshot := payment.ProviderPayload[paymentcontract.GatewayPayloadFiatCurrencySent]; !hasSnapshot {
			verifiedLegacyCurrency = strings.ToUpper(strings.TrimSpace(result.Currency))
			log.Warnw(
				"payment_webhook_legacy_dujiaopay_currency_adoption",
				"payment_id", payment.ID,
				"stored_currency", payment.Currency,
				"verified_currency", verifiedLegacyCurrency,
			)
		}
	}

	callbackInput := PaymentCallbackInput{
		PaymentID:   payment.ID,
		OrderNo:     result.OrderNo,
		ChannelID:   channel.ID,
		Status:      result.Status,
		ProviderRef: pickFirstNonEmpty(result.ProviderRef, payment.ProviderRef),
		Amount:      result.Amount,
		Currency:    strings.ToUpper(strings.TrimSpace(result.Currency)),
		PaidAt:      result.PaidAt,
		Payload:     payload,

		verifiedLegacyDujiaoPayCurrency: verifiedLegacyCurrency,
	}

	updated, err := s.HandleCallback(callbackInput)
	if err != nil {
		log.Errorw("payment_webhook_callback_apply_failed",
			"channel_id", channel.ID,
			"payment_id", payment.ID,
			"order_no", result.OrderNo,
			"provider_ref", result.ProviderRef,
			"status", result.Status,
			"error", err,
		)
		return nil, result.Status, err
	}
	log.Infow("payment_webhook_processed",
		"channel_id", channel.ID,
		"payment_id", updated.ID,
		"order_no", result.OrderNo,
		"provider_ref", result.ProviderRef,
		"status", updated.Status,
	)
	return updated, result.Status, nil
}

// supportsBlindWebhookCandidateMatching 标识哪些 channel_type 的 webhook 验签
// 具备"用任意 channel 的 config 试错即可确认归属"的能力。
//
// 微信(api_v3_key + AES-GCM auth tag)和 stripe(endpoint_secret + HMAC)
// 都满足:错配的密钥会在解密/验签阶段失败,正确的密钥才能成功;
// paypal 的 webhook 验签依赖 webhook_id 字段语义,无法盲匹配。
func supportsBlindWebhookCandidateMatching(channelType string) bool {
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case constants.PaymentChannelTypeWechat, constants.PaymentChannelTypeStripe:
		return true
	default:
		return false
	}
}

// findWebhookPayment 通过 webhook result 反查 payment。
// 优先用 OrderNo（= GatewayOrderNo，商户单号），次选 ProviderRef（网关流水号）。
func (s *PaymentService) findWebhookPayment(channelID uint, result *paymentcontract.GatewayCallbackResult) (*paymentdomain.Payment, error) {
	if result == nil {
		return nil, ErrPaymentNotFound
	}
	if orderNo := strings.TrimSpace(result.OrderNo); orderNo != "" {
		payment, err := s.paymentRepo.GetByGatewayOrderNo(orderNo)
		if err == nil && payment != nil && payment.ChannelID == channelID {
			return payment, nil
		}
	}
	if providerRef := strings.TrimSpace(result.ProviderRef); providerRef != "" {
		payment, err := s.paymentRepo.GetLatestByProviderRef(providerRef)
		if err == nil && payment != nil && payment.ChannelID == channelID {
			return payment, nil
		}
	}
	return nil, ErrPaymentNotFound
}

// HandleWechatWebhook 处理微信支付回调。
// P1.2c Task 6: 退化为 thin wrapper，通过 handleWebhookViaRegistry 路由解析。
func (s *PaymentService) HandleWechatWebhook(input WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	return s.handleWebhookViaRegistry(
		input,
		constants.PaymentProviderOfficial,
		constants.PaymentChannelTypeWechat,
	)
}

// HandleStripeWebhook 处理 Stripe webhook。
// P1.2c Task 6: 退化为 thin wrapper，通过 handleWebhookViaRegistry 路由解析。
func (s *PaymentService) HandleStripeWebhook(input WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	return s.handleWebhookViaRegistry(
		input,
		constants.PaymentProviderOfficial,
		constants.PaymentChannelTypeStripe,
	)
}

// HandleBinancepayWebhook 处理 Binance Pay webhook。
func (s *PaymentService) HandleBinancepayWebhook(input WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	return s.handleWebhookViaRegistry(
		input,
		constants.PaymentProviderOfficial,
		constants.PaymentChannelTypeBinancepay,
	)
}
