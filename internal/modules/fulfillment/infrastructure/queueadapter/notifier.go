package queueadapter

import (
	"strings"

	fulfillmentapp "github.com/dujiao-next/internal/modules/fulfillment/application"

	"github.com/dujiao-next/internal/queue"
)

// BotNotifier 把平台队列适配成交付通知端口。
type BotNotifier struct {
	client *queue.Client
}

var _ fulfillmentapp.BotNotifier = (*BotNotifier)(nil)

func NewBotNotifier(client *queue.Client) *BotNotifier {
	return &BotNotifier{client: client}
}

func (n *BotNotifier) EnqueueOrderFulfilled(telegramUserID string, orderID uint) error {
	if n == nil || n.client == nil {
		return nil
	}
	return n.client.EnqueueBotNotify(queue.BotNotifyPayload{
		EventType:      queue.BotNotifyEventOrderFulfilled,
		OrderID:        orderID,
		TelegramUserID: strings.TrimSpace(telegramUserID),
	})
}

// EnqueueOrderReplaced 推送“卡密已更换”的交付通知，机器人据此区分于首次交付。
func (n *BotNotifier) EnqueueOrderReplaced(telegramUserID string, orderID uint) error {
	if n == nil || n.client == nil {
		return nil
	}
	return n.client.EnqueueBotNotify(queue.BotNotifyPayload{
		EventType:      queue.BotNotifyEventOrderFulfilled,
		OrderID:        orderID,
		TelegramUserID: strings.TrimSpace(telegramUserID),
		Replaced:       true,
	})
}
