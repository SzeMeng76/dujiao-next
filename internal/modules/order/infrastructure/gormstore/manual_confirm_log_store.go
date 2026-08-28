package gormstore

import (
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"gorm.io/gorm"
)

// ManualConfirmLogStore GORM 人工确认支付审计日志存储。
type ManualConfirmLogStore struct {
	db *gorm.DB
}

func NewManualConfirmLogStore(db *gorm.DB) *ManualConfirmLogStore {
	return &ManualConfirmLogStore{db: db}
}

// Create 创建人工确认支付审计日志
func (r *ManualConfirmLogStore) Create(log *orderdomain.OrderManualConfirmLog) error {
	if log == nil {
		return nil
	}
	return r.db.Create(log).Error
}

// ListByOrderID 查询某订单的人工确认支付历史记录
func (r *ManualConfirmLogStore) ListByOrderID(orderID uint) ([]orderdomain.OrderManualConfirmLog, error) {
	logs := make([]orderdomain.OrderManualConfirmLog, 0)
	if err := r.db.Where("order_id = ?", orderID).Order("id DESC").Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}
