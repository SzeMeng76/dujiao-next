package orderadapter

import (
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
)

// Adapter 把订单 Store 适配为工单模块所需的 OrderOwnershipChecker。
type Adapter struct {
	orders ordercontract.Store
}

// New 创建订单归属校验适配器。
func New(orders ordercontract.Store) *Adapter {
	return &Adapter{orders: orders}
}

// ExistsForUser 校验订单号是否存在且属于该用户，返回订单ID。
func (a *Adapter) ExistsForUser(orderNo string, userID uint) (uint, bool, error) {
	if a.orders == nil {
		return 0, false, nil
	}
	order, err := a.orders.GetByOrderNoAndUser(orderNo, userID)
	if err != nil {
		return 0, false, err
	}
	if order == nil {
		return 0, false, nil
	}
	return order.ID, true, nil
}
