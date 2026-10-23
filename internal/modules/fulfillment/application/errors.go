package application

import (
	"errors"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	orderapp "github.com/dujiao-next/internal/modules/order/application"
)

var (
	ErrFulfillmentInvalid      = productcontract.ErrFulfillmentInvalid
	ErrFulfillmentExists       = errors.New("fulfillment exists")
	ErrFulfillmentCreateFailed = errors.New("fulfillment create failed")
	ErrFulfillmentNotAuto      = errors.New("fulfillment not auto")
	ErrFulfillmentNotFound     = errors.New("fulfillment not found")
	ErrFulfillmentUpdateFailed = errors.New("fulfillment update failed")
	ErrOrderNotFound           = orderapp.ErrOrderNotFound
	ErrOrderFetchFailed        = orderapp.ErrOrderFetchFailed
	ErrOrderStatusInvalid      = orderapp.ErrOrderStatusInvalid
	ErrOrderUpdateFailed       = orderapp.ErrOrderUpdateFailed
	ErrCardSecretInsufficient  = orderapp.ErrCardSecretInsufficient
)
