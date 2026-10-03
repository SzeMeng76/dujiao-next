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

// ResolveOrderNos 按订单ID批量解析订单号，仅用于工单响应展示。
// 查不到的 ID 不会出现在结果中（订单被硬删除时属正常情况）。
func (a *Adapter) ResolveOrderNos(ids []uint) (map[uint]string, error) {
	if a.orders == nil || len(ids) == 0 {
		return nil, nil
	}
	orders, err := a.orders.GetByIDs(ids)
	if err != nil {
		return nil, err
	}
	result := make(map[uint]string, len(orders))
	for _, order := range orders {
		if order.OrderNo == "" {
			continue
		}
		result[order.ID] = order.OrderNo
	}
	return result, nil
}
