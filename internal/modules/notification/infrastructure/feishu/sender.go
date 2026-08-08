package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dujiao-next/internal/modules/notification/contract"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const feishuRequestTimeout = 10 * time.Second

type messageClient interface {
	SendText(ctx context.Context, receiveIDType, receiveID, message string) error
}

type clientFactory func(appID, appSecret string) messageClient

// Sender 使用飞书官方 SDK 通过自建应用机器人发送通知。
// 它会复用当前凭据对应的 SDK Client，从而复用 SDK 内置的 tenant token 缓存。
type Sender struct {
	mu        sync.Mutex
	appID     string
	appSecret string
	client    messageClient
	factory   clientFactory
}

var _ contract.FeishuSender = (*Sender)(nil)

// New 创建飞书机器人通知发送器。
func New() *Sender {
	return newSender(func(appID, appSecret string) messageClient {
		return &sdkMessageClient{client: lark.NewClient(
			appID,
			appSecret,
			lark.WithReqTimeout(feishuRequestTimeout),
			lark.WithLogLevel(larkcore.LogLevelError),
		)}
	})
}

func newSender(factory clientFactory) *Sender {
	if factory == nil {
		panic("feishu notification sender: client factory is nil")
	}
	return &Sender{factory: factory}
}

// SendMessage 向指定飞书用户或群聊发送纯文本消息。
func (s *Sender) SendMessage(ctx context.Context, appID, appSecret, receiveIDType, receiveID, message string) error {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	receiveIDType = strings.ToLower(strings.TrimSpace(receiveIDType))
	receiveID = strings.TrimSpace(receiveID)
	message = strings.TrimSpace(message)
	if s == nil || appID == "" || appSecret == "" || receiveID == "" || message == "" || !isSupportedReceiveIDType(receiveIDType) {
		return contract.ErrConfigInvalid
	}
	return s.clientFor(appID, appSecret).SendText(ctx, receiveIDType, receiveID, message)
}

func (s *Sender) clientFor(appID, appSecret string) messageClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || s.appID != appID || s.appSecret != appSecret {
		s.client = s.factory(appID, appSecret)
		s.appID = appID
		s.appSecret = appSecret
	}
	return s.client
}

func isSupportedReceiveIDType(value string) bool {
	switch value {
	case "chat_id", "open_id", "user_id", "union_id", "email":
		return true
	default:
		return false
	}
}

type sdkMessageClient struct {
	client *lark.Client
}

func (c *sdkMessageClient) SendText(ctx context.Context, receiveIDType, receiveID, message string) error {
	content, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: message})
	if err != nil {
		return fmt.Errorf("%w: encode feishu message: %v", contract.ErrSendFailed, err)
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("text").
			Content(string(content)).
			Build()).
		Build()
	resp, err := c.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("%w: feishu sdk request: %v", contract.ErrSendFailed, err)
	}
	if resp == nil {
		return fmt.Errorf("%w: empty feishu response", contract.ErrSendFailed)
	}
	if !resp.Success() {
		requestID := ""
		if resp.ApiResp != nil {
			requestID = resp.RequestId()
		}
		return fmt.Errorf(
			"%w: feishu api code=%d msg=%s request_id=%s",
			contract.ErrSendFailed,
			resp.Code,
			strings.TrimSpace(resp.Msg),
			requestID,
		)
	}
	return nil
}
