package contract

import (
	"time"

	"github.com/dujiao-next/internal/modules/errorlog/domain"
)

// Filter 管理端查询条件。
type Filter struct {
	Page        int
	PageSize    int
	Level       string
	Category    string
	Method      string
	Path        string
	RequestID   string
	ClientIP    string
	Keyword     string // 在 message / detail 中模糊匹配
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

// Repository 错误日志存储。
//
// 只提供写入、查询和过期清理：日志是只追加的审计数据，不提供更新与单条删除，
// 避免出现「删掉不利证据」的操作面。
type Repository interface {
	// Create 单条写入，走异步管道时按批调用。
	Create(log *domain.ErrorLog) error
	// BatchCreate 批量写入，异步刷盘用。
	BatchCreate(logs []*domain.ErrorLog) error
	// ListAdmin 管理端分页查询。
	ListAdmin(filter Filter) ([]domain.ErrorLog, int64, error)
	// DeleteOlderThan 清理早于指定时间的记录，返回删除条数。
	DeleteOlderThan(before time.Time) (int64, error)
}
