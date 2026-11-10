// Package resellernotify 组装提现申请通知所需依赖，供分销（reseller）与
// 推广返利（affiliate）两个模块共同复用。放在 bootstrap 层是因为它需要
// notification 模块的服务，而模块之间不允许互相 import 内部包。
//
// 这里投递的是普通通知事件，走 notification.Service.Dispatch 的
// dispatchSingleEvent 分支 —— 与订单通知、工单通知同一条路径，发送到通知中心
// 配置的收件人（Telegram / 邮件 / 飞书）。它不是补货广播
// （dispatchRestockBroadcast），不会发到补货频道。
package resellernotify

import (
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	notificationcontract "github.com/dujiao-next/internal/modules/notification/contract"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// ScopeReseller 分销商提现申请。
const ScopeReseller = "reseller"

// ScopeAffiliate 推广返利用户提现申请。
const ScopeAffiliate = "affiliate"

// UserLookup 是查询申请人信息（用于通知文案）所需的最小端口。
type UserLookup interface {
	GetByID(id uint) (*userdomain.User, error)
}

// SettingsReader 是读取通知中心默认语言所需的最小端口。
type SettingsReader interface {
	GetNotificationCenterSetting() (settingsmessaging.NotificationCenterSetting, error)
}

// Notifier 提现申请通知器。
type Notifier struct {
	enqueuer notificationcontract.NotificationEnqueuer
	users    UserLookup
	settings SettingsReader
}

// New 创建提现通知器。各依赖均可为 nil（对应能力不可用，语言回退中文）。
func New(enqueuer notificationcontract.NotificationEnqueuer, users UserLookup, settings SettingsReader) *Notifier {
	return &Notifier{enqueuer: enqueuer, users: users, settings: settings}
}

// NotifyWithdrawSubmitted 投递「提现申请待审核」通知。
//
// 参数用基本类型而不是结构体：reseller 与 affiliate 两个模块各自声明了注入接口，
// 同一份结构体会因包路径不同而无法同时满足两边，用基本类型可以让一个实现同时
// 满足两个模块的接口。
//
// 投递失败只记日志，不影响提现主流程。
func (n *Notifier) NotifyWithdrawSubmitted(scope string, requestID, userID uint, amount, currency, channel, account string) {
	if n == nil || n.enqueuer == nil || requestID == 0 {
		return
	}
	locale := n.notificationLocale()

	err := n.enqueuer.Enqueue(notificationcontract.EnqueueInput{
		EventType: constants.NotificationEventWithdrawRequestSubmitted,
		BizType:   constants.NotificationBizTypeWithdraw,
		BizID:     requestID,
		Locale:    locale,
		Data: jsonmap.JSON{
			"withdraw_scope":  withdrawScopeLabel(scope, locale),
			"withdraw_id":     fmt.Sprintf("%d", requestID),
			"applicant_label": n.applicantLabel(userID, locale),
			"amount":          strings.TrimSpace(amount),
			"currency":        strings.ToUpper(strings.TrimSpace(currency)),
			"channel":         strings.TrimSpace(channel),
			"account":         strings.TrimSpace(account),
		},
	})
	if err != nil {
		logger.Warnw("withdraw_notify_enqueue_failed",
			"scope", scope,
			"withdraw_id", requestID,
			"error", err,
		)
	}
}

// notificationLocale 返回通知中心配置的默认语言，读取失败时回退中文。
func (n *Notifier) notificationLocale() string {
	if n == nil || n.settings == nil {
		return constants.LocaleZhCN
	}
	setting, err := n.settings.GetNotificationCenterSetting()
	if err != nil {
		return constants.LocaleZhCN
	}
	return settingsmessaging.NormalizeNotificationLocale(setting.DefaultLocale)
}

// applicantLabel 尽量取真实昵称/邮箱，取不到时用用户 ID 兜底。
func (n *Notifier) applicantLabel(userID uint, locale string) string {
	if n != nil && n.users != nil && userID != 0 {
		if user, err := n.users.GetByID(userID); err == nil && user != nil {
			if name := strings.TrimSpace(user.DisplayName); name != "" {
				return name
			}
			if email := strings.TrimSpace(user.Email); email != "" {
				return email
			}
		}
	}
	if locale == constants.LocaleEnUS {
		return fmt.Sprintf("User #%d", userID)
	}
	return fmt.Sprintf("用户#%d", userID)
}

// withdrawScopeLabel 把提现类别转换为可读文案。
func withdrawScopeLabel(scope, locale string) string {
	affiliate := strings.EqualFold(strings.TrimSpace(scope), ScopeAffiliate)
	switch locale {
	case constants.LocaleZhTW:
		if affiliate {
			return "推廣返利"
		}
		return "分銷商"
	case constants.LocaleEnUS:
		if affiliate {
			return "Affiliate"
		}
		return "Reseller"
	default:
		if affiliate {
			return "推广返利"
		}
		return "分销商"
	}
}
