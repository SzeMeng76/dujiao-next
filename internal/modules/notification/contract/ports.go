package contract

import (
	"context"
	"time"

	dashboardcontract "github.com/dujiao-next/internal/modules/dashboard/contract"
	"github.com/dujiao-next/internal/modules/notification/domain"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	settingsstorefront "github.com/dujiao-next/internal/modules/settings/schema/storefront"
	"github.com/dujiao-next/internal/queue"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
)

type NotificationCenterSetting = settingsmessaging.NotificationCenterSetting
type DashboardSetting = settingsstorefront.DashboardSetting
type DashboardAlertSetting = settingsstorefront.DashboardAlertSetting

type SettingsReader interface {
	GetNotificationCenterSetting() (settingsmessaging.NotificationCenterSetting, error)
	GetDashboardSetting() (settingsstorefront.DashboardSetting, error)
}

type EmailSender interface {
	SendCustomEmail(toEmail, subject, body string) error
}

// OrderStatusEmailInput carries the order facts required to render an email.
type OrderStatusEmailInput struct {
	OrderNo           string
	Status            string
	Amount            money.Amount
	RefundAmount      money.Amount
	RefundReason      string
	Currency          string
	SiteName          string
	SiteURL           string
	FulfillmentInfo   string
	Instructions      string
	IsGuest           bool
	AttachmentName    string
	AttachmentContent string
}

type DispatchQueue interface {
	EnqueueNotificationDispatch(payload queue.NotificationDispatchPayload, maxRetry int) error
}

type DashboardAlertReader interface {
	LoadDashboardAlertSetting() settingsstorefront.DashboardAlertSetting
	GetInventoryAlertItems(ctx context.Context, lowStockThreshold int64) ([]dashboardcontract.InventoryAlertRow, error)
	GetPaymentOrderAlertCounts(ctx context.Context, startAt, endAt time.Time) (dashboardcontract.PaymentOrderAlertCountsRow, error)
}

type TelegramSender interface {
	SendMessage(ctx context.Context, chatID, message string) error
	SendMessageWithOptions(ctx context.Context, options TelegramSendOptions) error
}

// TelegramSendOptions Telegram 发送参数（用于补货广播等需要附加 inline 按钮的场景）。
// 字段布局需与 internal/modules/telegram/notify/contract.SendOptions 保持一致，
// 以便适配器可直接转换而不引入循环依赖。
type TelegramSendOptions struct {
	ChatID                string
	Message               string
	ParseMode             string
	DisableWebPagePreview bool
	AttachmentURL         string
	AttachmentDisplayName string
	ReplyMarkup           map[string]interface{}
}

type LogRepository interface {
	Create(log *domain.NotificationLog) error
	ListAdmin(filter LogListFilter) ([]domain.NotificationLog, int64, error)
}

type LogListFilter struct {
	Page        int
	PageSize    int
	Channel     string
	Status      string
	EventType   string
	IsTest      *bool
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

// EnqueueInput 描述一个待投递的业务通知事件。
type EnqueueInput struct {
	EventType string
	BizType   string
	BizID     uint
	Locale    string
	Force     bool
	Data      jsonmap.JSON
}

type NotificationEnqueuer interface {
	Enqueue(input EnqueueInput) error
}

// TestSendInput 描述后台通知中心的一次测试发送。
type TestSendInput struct {
	Channel   string
	Target    string
	Scene     string
	Locale    string
	Variables map[string]interface{}
}

type TestSender interface {
	SendTest(context.Context, TestSendInput) error
}
