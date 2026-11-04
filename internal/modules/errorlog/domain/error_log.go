package domain

import (
	"strings"
	"time"
)

// ErrorLog 应用错误日志。
//
// 只记录「已经发生并对外表现为失败」的错误（5xx / panic），不记录业务校验失败等
// 预期内的 4xx —— 那些属于正常交互，写进来只会把真正的问题淹掉。
type ErrorLog struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	Level      string    `gorm:"type:varchar(10);index;not null;default:'error'" json:"level"`
	Category   string    `gorm:"type:varchar(40);index;not null;default:'system'" json:"category"`
	Message    string    `gorm:"type:text;not null;default:''" json:"message"`
	Detail     string    `gorm:"type:text" json:"detail"`
	Method     string    `gorm:"type:varchar(10);index;not null;default:''" json:"method"`
	Path       string    `gorm:"type:varchar(255);index;not null;default:''" json:"path"`
	StatusCode int       `gorm:"index;not null;default:0" json:"status_code"`
	ClientIP   string    `gorm:"type:varchar(64);index" json:"client_ip"`
	UserAgent  string    `gorm:"type:text" json:"user_agent"`
	RequestID  string    `gorm:"type:varchar(64);index" json:"request_id"`
	UserID     *uint     `gorm:"index" json:"user_id,omitempty"`
	AdminID    *uint     `gorm:"index" json:"admin_id,omitempty"`
	CreatedAt  time.Time `gorm:"index;not null" json:"created_at"`
}

// TableName 指定表名
func (ErrorLog) TableName() string {
	return "error_logs"
}

// 日志级别
const (
	LevelError = "error"
	LevelWarn  = "warn"
)

// 错误分类 —— 由请求路径推导，便于在后台按模块筛选。
const (
	CategoryPayment  = "payment"  // 支付 / 回调 / webhook
	CategoryOrder    = "order"    // 订单 / 履约 / 退款
	CategoryUpstream = "upstream" // 上游对接
	CategoryChannel  = "channel"  // 渠道 API
	CategoryAuth     = "auth"     // 登录 / 鉴权
	CategorySystem   = "system"   // 其余
)

// CategoryFromPath 按请求路径推导错误分类。
// 未知路径一律归入 system，绝不返回空串 —— 空分类会让后台筛选漏掉记录。
func CategoryFromPath(path string) string {
	switch {
	case containsAny(path, "/payment", "/callback", "/webhook", "/epay"):
		return CategoryPayment
	case containsAny(path, "/orders", "/order/", "/fulfillment", "/refund"):
		return CategoryOrder
	case containsAny(path, "/upstream"):
		return CategoryUpstream
	case containsAny(path, "/channel"):
		return CategoryChannel
	case containsAny(path, "/login", "/auth", "/2fa", "/authz"):
		return CategoryAuth
	default:
		return CategorySystem
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
