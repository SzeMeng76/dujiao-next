package gormstore

import (
	"time"

	"github.com/dujiao-next/internal/modules/auditlog/contract"
	"github.com/dujiao-next/internal/modules/auditlog/domain"
	"github.com/dujiao-next/internal/persistence/gormutil"

	"gorm.io/gorm"
)

// AdminLoginStore GORM 管理员登录日志存储。
type AdminLoginStore struct {
	db *gorm.DB
}

func NewAdminLoginStore(db *gorm.DB) *AdminLoginStore {
	return &AdminLoginStore{db: db}
}

// Create 写入一条日志
func (r *AdminLoginStore) Create(log *domain.AdminLoginLog) error {
	if log == nil {
		return nil
	}
	return r.db.Create(log).Error
}

// List 分页查询
func (r *AdminLoginStore) List(filter contract.AdminLoginFilter) ([]domain.AdminLoginLog, int64, error) {
	query := r.db.Model(&domain.AdminLoginLog{})
	if filter.AdminID != nil {
		query = query.Where("admin_id = ?", *filter.AdminID)
	}
	if filter.Username != "" {
		query = query.Where("username = ?", filter.Username)
	}
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	query = gormutil.ApplyPagination(query, filter.Page, filter.PageSize)

	logs := make([]domain.AdminLoginLog, 0)
	if err := query.Order("id desc").Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// DeleteOlderThan 分批删除创建时间早于 before 的管理员登录日志，返回删除条数。
func (r *AdminLoginStore) DeleteOlderThan(before time.Time) (int64, error) {
	return gormutil.DeleteOlderThanBatched(r.db, &domain.AdminLoginLog{}, before, gormutil.DefaultRetentionBatchSize)
}

var _ contract.AdminLoginRepository = (*AdminLoginStore)(nil)
