package contract

// SendOptions 是 Telegram 消息发送参数。
type SendOptions struct {
	ChatID                string
	Message               string
	ParseMode             string
	DisableWebPagePreview bool
	AttachmentURL         string
	AttachmentDisplayName string
	// ReplyMarkup Telegram inline 键盘等附加结构（如补货通知的「立即购买」按钮）。
	ReplyMarkup map[string]interface{}
}
