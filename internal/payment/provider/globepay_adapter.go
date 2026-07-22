package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/payment/globepay"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/shopspring/decimal"
)

type globepayAdapter struct{}

func NewGlobepayAdapter() Provider { return &globepayAdapter{} }

var (
	_ Provider         = (*globepayAdapter)(nil)
	_ CallbackVerifier = (*globepayAdapter)(nil)
)

func (a *globepayAdapter) Type() string {
	return constants.PaymentProviderGlobepay + ":"
}

func (a *globepayAdapter) ValidateConfig(raw jsonmap.JSON, channelType string) error {
	if channelType != "" && !globepay.IsSupportedChannelType(channelType) {
		return fmt.Errorf("%w: globepay channel_type %s", ErrUnsupportedChannel, channelType)
	}
	cfg, err := globepay.ParseConfig(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrConfigInvalid, err)
	}
	if err := globepay.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("%w: %v", ErrConfigInvalid, err)
	}
	return nil
}

func (a *globepayAdapter) CreatePayment(ctx context.Context, raw jsonmap.JSON, input CreateInput) (*CreateResult, error) {
	if !globepay.IsSupportedChannelType(input.ChannelType) {
		return nil, fmt.Errorf("%w: globepay channel_type %s", ErrUnsupportedChannel, input.ChannelType)
	}
	cfg, err := globepay.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConfigInvalid, err)
	}

	notifyURL := strings.TrimSpace(input.NotifyURL)
	if notifyURL == "" {
		notifyURL = cfg.NotifyURL
	}
	returnURL := strings.TrimSpace(input.ReturnURL)
	if returnURL == "" {
		returnURL = cfg.ReturnURL
	}
	returnURL = appendQueryParams(returnURL, input.ReturnURLQuery)

	interactionMode, _ := input.Extra["interaction_mode"].(string)
	native := globepay.CreateInput{
		OrderNo:         input.OrderNo,
		Amount:          input.Amount.Decimal.String(),
		Subject:         input.Subject,
		ChannelType:     input.ChannelType,
		InteractionMode: interactionMode,
		NotifyURL:       notifyURL,
		ReturnURL:       returnURL,
	}

	result, err := globepay.CreatePayment(ctx, cfg, native)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}

	payload := jsonmap.JSON{}
	if result.Raw != nil {
		payload = jsonmap.JSON(result.Raw)
	}

	return &CreateResult{
		ProviderRef:  result.TradeNo,
		RedirectURL:  result.PayURL,
		QRCodeURL:    result.QRCode,
		Payload:      payload,
		AmountSent:   input.Amount.Decimal.String(),
		CurrencySent: input.Currency,
	}, nil
}

func (a *globepayAdapter) VerifyCallback(raw jsonmap.JSON, form map[string][]string, body []byte) (*CallbackResult, error) {
	cfg, err := globepay.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConfigInvalid, err)
	}

	data := make(map[string]string, len(form))
	for k, v := range form {
		if len(v) > 0 {
			data[k] = v[0]
		}
	}

	if err := globepay.VerifyCallback(cfg, data); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	}

	orderNo := data["partner_order_id"]
	providerRef := data["order_id"]
	amountStr := data["price"]

	amount := models.Money{}
	if s := strings.TrimSpace(amountStr); s != "" {
		if d, parseErr := decimal.NewFromString(s); parseErr == nil {
			// Globepay 回调金额单位是分
			amount = models.NewMoneyFromDecimal(d.Div(decimal.NewFromInt(100)))
		}
	}

	return &CallbackResult{
		OrderNo:     orderNo,
		ProviderRef: providerRef,
		Status:      constants.PaymentStatusSuccess,
		Amount:      amount,
		Currency:    "CNY",
		PaidAt:      nil,
		Payload:     formToJSON(form),
	}, nil
}
