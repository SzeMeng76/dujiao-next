package fulfillmentwiring

import (
	"errors"
	"fmt"

	fulfillmentapp "github.com/dujiao-next/internal/modules/fulfillment/application"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderapp "github.com/dujiao-next/internal/modules/order/application"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	fulfillmenttransport "github.com/dujiao-next/internal/modules/fulfillment/transport/http"
)

type fulfillmentManualCreatorAdapter struct {
	svc *fulfillmentapp.Service
}

func (a fulfillmentManualCreatorAdapter) CreateManual(input fulfillmenttransport.CreateManualInput) (*fulfillmentdomain.Fulfillment, error) {
	res, err := a.svc.CreateManual(fulfillmentapp.CreateManualInput{
		OrderID:      input.OrderID,
		AdminID:      input.AdminID,
		Payload:      input.Payload,
		DeliveryData: input.DeliveryData,
	})
	return res, mapFulfillmentTransportError(err)
}

type fulfillmentSecretReplacerAdapter struct {
	svc *fulfillmentapp.Service
}

func (a fulfillmentSecretReplacerAdapter) ReplaceSecrets(input fulfillmenttransport.ReplaceSecretsInput) (*fulfillmentdomain.Fulfillment, int, error) {
	pairs := make([]fulfillmentapp.SecretReplacement, 0, len(input.Pairs))
	for _, pair := range input.Pairs {
		pairs = append(pairs, fulfillmentapp.SecretReplacement{OldSecretID: pair.OldSecretID, NewSecretID: pair.NewSecretID})
	}
	res, replaced, err := a.svc.ReplaceSecrets(fulfillmentapp.ReplaceSecretsInput{
		OrderID: input.OrderID,
		AdminID: input.AdminID,
		Secrets: input.Secrets,
		Pairs:   pairs,
		Reason:  input.Reason,
	})
	return res, replaced, mapFulfillmentTransportError(err)
}

func (a fulfillmentSecretReplacerAdapter) ListReplaceCandidates(orderID uint) (*fulfillmenttransport.ReplaceCandidatesView, error) {
	res, err := a.svc.ListReplaceCandidates(orderID)
	if err != nil {
		return nil, mapFulfillmentTransportError(err)
	}
	view := &fulfillmenttransport.ReplaceCandidatesView{
		Delivered: make([]fulfillmenttransport.DeliveredSecretView, 0, len(res.Delivered)),
		Stock:     make([]fulfillmenttransport.StockSecretView, 0, len(res.Stock)),
	}
	for _, item := range res.Delivered {
		view.Delivered = append(view.Delivered, fulfillmenttransport.DeliveredSecretView{
			ID: item.ID, Secret: item.Secret, ProductID: item.ProductID, SKUID: item.SKUID, UsedAt: item.UsedAt,
		})
	}
	for _, item := range res.Stock {
		view.Stock = append(view.Stock, fulfillmenttransport.StockSecretView{
			ID: item.ID, Secret: item.Secret, ProductID: item.ProductID, SKUID: item.SKUID, CreatedAt: item.CreatedAt,
		})
	}
	return view, nil
}

type fulfillmentAdminOrderAdapter struct {
	orders *orderapp.OrderService
}

func (a fulfillmentAdminOrderAdapter) GetOrderForAdmin(orderID uint) (*orderdomain.Order, error) {
	order, err := a.orders.GetOrderForAdmin(orderID)
	return order, mapFulfillmentTransportError(err)
}

func mapFulfillmentTransportError(err error) error {
	if err == nil {
		return nil
	}
	for _, mapping := range []struct {
		source error
		target error
	}{
		{fulfillmentapp.ErrFulfillmentExists, fulfillmenttransport.ErrFulfillmentExists},
		{fulfillmentapp.ErrFulfillmentInvalid, fulfillmenttransport.ErrFulfillmentInvalid},
		{fulfillmentapp.ErrOrderStatusInvalid, fulfillmenttransport.ErrOrderStatusInvalid},
		{fulfillmentapp.ErrOrderNotFound, fulfillmenttransport.ErrOrderNotFound},
		{fulfillmentapp.ErrFulfillmentNotFound, fulfillmenttransport.ErrFulfillmentNotFound},
		{fulfillmentapp.ErrFulfillmentNotAuto, fulfillmenttransport.ErrFulfillmentNotAuto},
		{fulfillmentapp.ErrFulfillmentUpdateFailed, fulfillmenttransport.ErrFulfillmentUpdateFailed},
		{fulfillmentapp.ErrCardSecretInsufficient, fulfillmenttransport.ErrCardSecretInsufficient},
	} {
		if errors.Is(err, mapping.source) {
			return fmt.Errorf("%w: %v", mapping.target, err)
		}
	}
	return err
}
