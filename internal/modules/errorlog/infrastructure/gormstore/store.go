package gormstore

import (
	"time"

	"github.com/dujiao-next/internal/modules/errorlog/contract"
	"github.com/dujiao-next/internal/modules/errorlog/domain"
	"github.com/dujiao-next/internal/persistence/gormutil"

	"gorm.io/gorm"
)

// Store 错误日志 GORM 存储。
type Store struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Create 单条写入。
func (s *Store) Create(log *domain.ErrorLog) error {
	if log == nil {
		return nil
	}
	return s.db.Create(log).Error
}

// BatchCreate 批量写入。
//
// 分批是必需的：SQLite 变量上限是 999，单条记录约 12 列，
// 一次塞太多记录会直接报 "too many SQL variables"。
func (s *Store) BatchCreate(logs []*domain.ErrorLog) error {
	if len(logs) == 0 {
		return nil
	}
	const batchSize = 50
	return s.db.CreateInBatches(logs, batchSize).Error
}

// ListAdmin 管理端分页查询。
func (s *Store) ListAdmin(filter contract.Filter) ([]domain.ErrorLog, int64, error) {
	query := s.db.Model(&domain.ErrorLog{})
	if filter.Level != "" {
		query = query.Where("level = ?", filter.Level)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if filter.Method != "" {
		query = query.Where("method = ?", filter.Method)
	}
	if filter.Path != "" {
		query = query.Where("path LIKE ?", "%"+filter.Path+"%")
	}
	if filter.RequestID != "" {
		query = query.Where("request_id = ?", filter.RequestID)
	}
	if filter.ClientIP != "" {
		query = query.Where("client_ip = ?", filter.ClientIP)
	}
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		query = query.Where("message LIKE ? OR detail LIKE ?", like, like)
	}
	if filter.CreatedFrom != nil {
		query = query.Where("created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("created_at <= ?", *filter.CreatedTo)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	logs := make([]domain.ErrorLog, 0)
	query = gormutil.ApplyPagination(query, filter.Page, filter.PageSize)
	if err := query.Order("id DESC").Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// DeleteOlderThan 清理过期记录。
func (s *Store) DeleteOlderThan(before time.Time) (int64, error) {
	result := s.db.Where("created_at < ?", before).Delete(&domain.ErrorLog{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

var _ contract.Repository = (*Store)(nil)
