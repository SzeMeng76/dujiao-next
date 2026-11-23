package application

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	fulfillmentcontract "github.com/dujiao-next/internal/modules/fulfillment/contract"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	externalidentitycontract "github.com/dujiao-next/internal/modules/identity/externalidentity/contract"
	orderapp "github.com/dujiao-next/internal/modules/order/application"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	settingsapp "github.com/dujiao-next/internal/modules/settings/application"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// Service 编排人工交付与自动交付。
type Service struct {
	orderStore            ordercontract.Store
	fulfillmentRepo       fulfillmentcontract.Store
	orderQueue            ordercontract.Queue
	botNotifier           BotNotifier
	settingService        *settingsapp.Service
	defaultEmailConfig    config.EmailConfig
	downstreamCallbackSvc DownstreamCallbackEnqueuer
	userOAuthIdentityRepo externalidentitycontract.Store
}

type BotNotifier interface {
	EnqueueOrderFulfilled(telegramUserID string, orderID uint) error
	EnqueueOrderReplaced(telegramUserID string, orderID uint) error
}

// replacedStatusEmailQueue 是可选能力：支持入队“卡密已更换”标记的订单邮件队列。
type replacedStatusEmailQueue interface {
	EnqueueStatusEmailReplaced(orderID uint, status string) error
}

// replacedEmailQueue 把 EnqueueStatusEmail 改写为带“已更换”标记的版本，复用现有的收件人/开关资格判断。
type replacedEmailQueue struct {
	ordercontract.Queue
	replaced replacedStatusEmailQueue
}

func (q replacedEmailQueue) EnqueueStatusEmail(orderID uint, status string) error {
	return q.replaced.EnqueueStatusEmailReplaced(orderID, status)
}

type DownstreamCallbackEnqueuer interface {
	EnqueueCallback(orderID uint)
}

// SetDownstreamCallbackService 设置下游回调服务（解决循环依赖）
func (s *Service) SetDownstreamCallbackService(svc DownstreamCallbackEnqueuer) {
	s.downstreamCallbackSvc = svc
}

// Options 汇总交付用例依赖。
type Options struct {
	OrderStore            ordercontract.Store
	FulfillmentStore      fulfillmentcontract.Store
	OrderQueue            ordercontract.Queue
	BotNotifier           BotNotifier
	SettingService        *settingsapp.Service
	DefaultEmailConfig    config.EmailConfig
	ExternalIdentityStore externalidentitycontract.Store
}

// New 创建交付服务。
func New(
	opts Options,
) *Service {
	return &Service{
		orderStore:            opts.OrderStore,
		fulfillmentRepo:       opts.FulfillmentStore,
		orderQueue:            opts.OrderQueue,
		botNotifier:           opts.BotNotifier,
		settingService:        opts.SettingService,
		defaultEmailConfig:    opts.DefaultEmailConfig,
		userOAuthIdentityRepo: opts.ExternalIdentityStore,
	}
}

// CreateManualInput 创建人工交付输入
type CreateManualInput struct {
	OrderID      uint
	AdminID      uint
	Payload      string
	DeliveryData jsonmap.JSON
	DeliveredAt  *time.Time
}

// CreateManual 创建人工交付
func (s *Service) CreateManual(input CreateManualInput) (*fulfillmentdomain.Fulfillment, error) {
	if input.OrderID == 0 || input.AdminID == 0 {
		return nil, ErrFulfillmentInvalid
	}
	payload := strings.TrimSpace(input.Payload)
	deliveryData := normalizeManualDeliveryData(input.DeliveryData)
	if payload == "" && len(deliveryData) == 0 {
		return nil, ErrFulfillmentInvalid
	}
	if payload == "" {
		payload = buildManualDeliveryPayload(deliveryData)
	}
	ftype := constants.FulfillmentTypeManual

	order, err := s.orderStore.GetByID(input.OrderID)
	if err != nil {
		return nil, ErrOrderFetchFailed
	}
	if order == nil {
		return nil, ErrOrderNotFound
	}
	if order.ParentID == nil && len(order.Children) > 0 {
		return nil, ErrFulfillmentInvalid
	}
	if order.Status != constants.OrderStatusPaid && order.Status != constants.OrderStatusFulfilling {
		return nil, ErrOrderStatusInvalid
	}

	now := time.Now()
	deliveredAt := input.DeliveredAt
	if deliveredAt == nil {
		deliveredAt = &now
	}

	var created *fulfillmentdomain.Fulfillment
	err = s.orderStore.WithinTransaction(func(tx ordercontract.Transaction) error {
		// 事务内锁住订单并复核状态：事务外的检查之后退款可能已经提交。
		locked, err := tx.Orders().GetByIDForUpdate(input.OrderID)
		if err != nil {
			return err
		}
		if locked == nil || (locked.Status != constants.OrderStatusPaid && locked.Status != constants.OrderStatusFulfilling) {
			return ErrOrderStatusInvalid
		}
		if _, found, err := tx.Fulfillments().FindByOrderIDForUpdate(input.OrderID); err != nil {
			return err
		} else if found {
			return ErrFulfillmentExists
		}

		fulfillment := &fulfillmentdomain.Fulfillment{
			OrderID:       input.OrderID,
			Type:          ftype,
			Status:        constants.FulfillmentStatusDelivered,
			Payload:       payload,
			LogisticsJSON: deliveryData,
			DeliveredBy:   &input.AdminID,
			DeliveredAt:   deliveredAt,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		if err := tx.Fulfillments().Create(fulfillment); err != nil {
			return ErrFulfillmentCreateFailed
		}
		if err := tx.Orders().UpdateFields(order.ID, map[string]interface{}{
			"status":     constants.OrderStatusDelivered,
			"updated_at": now,
		}); err != nil {
			return ErrOrderUpdateFailed
		}
		created = fulfillment
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrFulfillmentExists) {
			return nil, ErrFulfillmentExists
		}
		if errors.Is(err, ErrOrderStatusInvalid) {
			return nil, ErrOrderStatusInvalid
		}
		if errors.Is(err, ErrOrderUpdateFailed) {
			return nil, ErrOrderUpdateFailed
		}
		return nil, ErrFulfillmentCreateFailed
	}
	if s.orderQueue != nil {
		if order.ParentID != nil {
			status, syncErr := orderapp.SyncParentStatus(s.orderStore, *order.ParentID, now)
			if syncErr != nil {
				logger.Warnw("fulfillment_sync_parent_status_failed",
					"order_id", order.ID,
					"parent_order_id", *order.ParentID,
					"target_status", constants.OrderStatusDelivered,
					"error", syncErr,
				)
			} else {
				if status == "" {
					status = constants.OrderStatusDelivered
				}
				if status != constants.OrderStatusCanceled {
					if _, err := orderapp.EnqueueStatusEmailTaskIfEligible(s.orderStore, s.orderQueue, s.settingService, s.defaultEmailConfig, *order.ParentID, status); err != nil {
						logger.Warnw("fulfillment_enqueue_status_email_failed",
							"order_id", order.ID,
							"target_order_id", *order.ParentID,
							"status", status,
							"error", err,
						)
					}
				}
			}
		} else {
			if _, err := orderapp.EnqueueStatusEmailTaskIfEligible(s.orderStore, s.orderQueue, s.settingService, s.defaultEmailConfig, input.OrderID, constants.OrderStatusDelivered); err != nil {
				logger.Warnw("fulfillment_enqueue_status_email_failed",
					"order_id", order.ID,
					"target_order_id", input.OrderID,
					"status", constants.OrderStatusDelivered,
					"error", err,
				)
			}
		}
	}
	// Telegram 通知：交付完成后推送给用户
	notifyOrderID := input.OrderID
	if order.ParentID != nil {
		notifyOrderID = *order.ParentID
	}
	go s.NotifyBotOrderFulfilled(order.UserID, notifyOrderID)
	// B 侧：人工交付完成后触发下游回调
	if s.downstreamCallbackSvc != nil {
		s.downstreamCallbackSvc.EnqueueCallback(input.OrderID)
	}
	return created, nil
}

// CreateAuto 自动交付
func (s *Service) CreateAuto(orderID uint) (*fulfillmentdomain.Fulfillment, error) {
	if orderID == 0 {
		return nil, ErrFulfillmentInvalid
	}

	order, err := s.orderStore.GetByID(orderID)
	if err != nil {
		return nil, ErrOrderFetchFailed
	}
	if order == nil {
		return nil, ErrOrderNotFound
	}
	if order.ParentID == nil && len(order.Children) > 0 {
		return nil, ErrFulfillmentInvalid
	}
	if order.Status != constants.OrderStatusPaid {
		return nil, ErrOrderStatusInvalid
	}
	if len(order.Items) == 0 {
		return nil, ErrFulfillmentInvalid
	}

	for _, item := range order.Items {
		if strings.TrimSpace(item.FulfillmentType) != constants.FulfillmentTypeAuto {
			return nil, ErrFulfillmentNotAuto
		}
	}

	now := time.Now()
	var fulfillment *fulfillmentdomain.Fulfillment
	err = s.orderStore.WithinTransaction(func(tx ordercontract.Transaction) error {
		// 事务内锁住订单并复核状态：事务外的检查之后退款可能已经提交。
		locked, err := tx.Orders().GetByIDForUpdate(orderID)
		if err != nil {
			return err
		}
		if locked == nil || locked.Status != constants.OrderStatusPaid {
			return ErrOrderStatusInvalid
		}
		if _, found, err := tx.Fulfillments().FindByOrderIDForUpdate(orderID); err != nil {
			return err
		} else if found {
			return ErrFulfillmentExists
		}

		secretRepo := tx.CardSecrets()
		reservedRows, err := secretRepo.ListByOrderAndStatus(orderID, cardsecretdomain.StatusReserved)
		if err != nil {
			return err
		}
		reservedByKey := make(map[string][]cardsecretdomain.Secret)
		for _, reserved := range reservedRows {
			key := orderdomain.ItemKey(reserved.ProductID, reserved.SKUID)
			reservedByKey[key] = append(reservedByKey[key], reserved)
		}
		var secrets []cardsecretdomain.Secret
		for _, item := range order.Items {
			if item.ProductID == 0 || item.Quantity <= 0 {
				return ErrFulfillmentInvalid
			}
			key := orderdomain.ItemKey(item.ProductID, item.SKUID)
			cachedReserved := reservedByKey[key]
			selected := make([]cardsecretdomain.Secret, 0, item.Quantity)
			if len(cachedReserved) > 0 {
				take := item.Quantity
				if len(cachedReserved) < take {
					take = len(cachedReserved)
				}
				selected = append(selected, cachedReserved[:take]...)
				reservedByKey[key] = cachedReserved[take:]
			}

			if len(selected) < item.Quantity {
				need := item.Quantity - len(selected)
				availableRows, err := secretRepo.ListAvailableByProduct(item.ProductID, item.SKUID, need)
				if err != nil {
					return err
				}
				selected = append(selected, availableRows...)
			}
			if len(selected) < item.Quantity {
				return ErrCardSecretInsufficient
			}
			secrets = append(secrets, selected...)
		}

		ids := make([]uint, 0, len(secrets))
		secretLines := make([]string, 0, len(secrets))
		for _, secret := range secrets {
			ids = append(ids, secret.ID)
			secretLines = append(secretLines, secret.Secret)
		}

		affected, err := secretRepo.MarkUsed(ids, orderID, now)
		if err != nil {
			return err
		}
		if int(affected) != len(ids) {
			return ErrCardSecretInsufficient
		}

		payload := strings.Join(secretLines, "\n")
		fulfillment = &fulfillmentdomain.Fulfillment{
			OrderID:     orderID,
			Type:        constants.FulfillmentTypeAuto,
			Status:      constants.FulfillmentStatusDelivered,
			Payload:     payload,
			DeliveredAt: &now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := tx.Fulfillments().Create(fulfillment); err != nil {
			return ErrFulfillmentCreateFailed
		}
		if err := tx.Orders().UpdateFields(orderID, map[string]interface{}{
			"status":     constants.OrderStatusCompleted,
			"updated_at": now,
		}); err != nil {
			return ErrOrderUpdateFailed
		}
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrFulfillmentExists):
			return nil, ErrFulfillmentExists
		case errors.Is(err, ErrOrderStatusInvalid):
			return nil, ErrOrderStatusInvalid
		case errors.Is(err, ErrCardSecretInsufficient):
			return nil, ErrCardSecretInsufficient
		case errors.Is(err, ErrOrderUpdateFailed):
			return nil, ErrOrderUpdateFailed
		case errors.Is(err, ErrFulfillmentNotAuto):
			return nil, ErrFulfillmentNotAuto
		default:
			return nil, ErrFulfillmentCreateFailed
		}
	}
	if s.orderQueue != nil {
		if order.ParentID != nil {
			status, syncErr := orderapp.SyncParentStatus(s.orderStore, *order.ParentID, now)
			if syncErr != nil {
				logger.Warnw("fulfillment_sync_parent_status_failed",
					"order_id", order.ID,
					"parent_order_id", *order.ParentID,
					"target_status", constants.OrderStatusCompleted,
					"error", syncErr,
				)
			} else {
				if status == "" {
					status = constants.OrderStatusCompleted
				}
				if status != constants.OrderStatusCanceled {
					if _, err := orderapp.EnqueueStatusEmailTaskIfEligible(s.orderStore, s.orderQueue, s.settingService, s.defaultEmailConfig, *order.ParentID, status); err != nil {
						logger.Warnw("fulfillment_enqueue_status_email_failed",
							"order_id", order.ID,
							"target_order_id", *order.ParentID,
							"status", status,
							"error", err,
						)
					}
				}
			}
		} else {
			if _, err := orderapp.EnqueueStatusEmailTaskIfEligible(s.orderStore, s.orderQueue, s.settingService, s.defaultEmailConfig, orderID, constants.OrderStatusCompleted); err != nil {
				logger.Warnw("fulfillment_enqueue_status_email_failed",
					"order_id", order.ID,
					"target_order_id", orderID,
					"status", constants.OrderStatusCompleted,
					"error", err,
				)
			}
		}
	}
	// Telegram 通知：交付完成后推送给用户
	notifyOrderID := orderID
	if order.ParentID != nil {
		notifyOrderID = *order.ParentID
	}
	go s.NotifyBotOrderFulfilled(order.UserID, notifyOrderID)
	// B 侧：自动交付完成后触发下游回调
	if s.downstreamCallbackSvc != nil {
		s.downstreamCallbackSvc.EnqueueCallback(orderID)
	}
	return fulfillment, nil
}

// ReplaceSecretsInput 管理员替换已交付订单中有问题的自动发货卡密。
type ReplaceSecretsInput struct {
	OrderID uint
	AdminID uint
	Secrets []string            // 要替换的旧卡密内容；为空表示替换该订单交付内容中的全部卡密
	Pairs   []SecretReplacement // 指定「旧卡密ID → 库存新卡密ID」；非空时忽略 Secrets
	Reason  string
}

// SecretReplacement 指定用库存中的哪条卡密替换交付内容里的哪一行。
// OldLineIndex 为 payload 按换行切分后的行号；传 -1 时回退到 OldSecretID（兼容旧调用方）。
type SecretReplacement struct {
	OldLineIndex int
	OldSecretID  uint
	NewSecretID  uint
}

// DeliveredSecret 订单当前交付内容中的一条卡密。
// LineIndex 是 payload 中的行号，也是替换时定位这一行的依据，不依赖卡密历史记录。
type DeliveredSecret struct {
	ID        uint
	LineIndex int
	Secret    string
	ProductID uint
	SKUID     uint
	UsedAt    *time.Time
}

// StockSecret 可用于替换的库存卡密。
type StockSecret struct {
	ID        uint
	Secret    string
	ProductID uint
	SKUID     uint
	CreatedAt time.Time
}

// ReplaceCandidates 替换卡密所需的候选数据：本单已交付卡密与同商品/SKU 的可用库存（最旧在前）。
type ReplaceCandidates struct {
	Delivered []DeliveredSecret
	Stock     []StockSecret
}

const replaceCandidateStockLimit = 100

// deliveredLine 描述交付内容中的一行及其对应的商品/SKU。
// 卡密历史被定时清理后，行号与订单项仍能还原规格，替换不再依赖 used 卡密记录。
type deliveredLine struct {
	LineIndex int
	Secret    string
	ProductID uint
	SKUID     uint
	UsedRowID uint
	UsedAt    *time.Time
}

// orderItemRefs 按订单项顺序展开商品/SKU，与自动交付 payload 的生成顺序一致。
func orderItemRefs(items []orderdomain.OrderItem) [][2]uint {
	refs := make([][2]uint, 0)
	for _, item := range items {
		quantity := item.Quantity
		if quantity < 0 {
			quantity = 0
		}
		for i := 0; i < quantity; i++ {
			refs = append(refs, [2]uint{item.ProductID, item.SKUID})
		}
	}
	return refs
}

// resolveDeliveredLines 把交付内容逐行映射回商品/SKU。used 卡密记录仍存在时优先采用它的商品/SKU，
// 并附带记录 ID 供追溯；记录已被清理时按订单项顺序还原。
func resolveDeliveredLines(payload string, items []orderdomain.OrderItem, usedRows []cardsecretdomain.Secret) []deliveredLine {
	refs := orderItemRefs(items)
	usedByText := make(map[string][]cardsecretdomain.Secret, len(usedRows))
	for _, row := range usedRows {
		text := strings.TrimSpace(row.Secret)
		if text == "" {
			continue
		}
		usedByText[text] = append(usedByText[text], row)
	}
	consumed := make(map[uint]bool, len(usedRows))
	lines := make([]deliveredLine, 0, len(refs))
	position := 0
	for idx, raw := range strings.Split(payload, "\n") {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		line := deliveredLine{LineIndex: idx, Secret: text}
		for _, row := range usedByText[text] {
			if consumed[row.ID] {
				continue
			}
			consumed[row.ID] = true
			line.UsedRowID = row.ID
			line.UsedAt = row.UsedAt
			line.ProductID = row.ProductID
			line.SKUID = row.SKUID
			break
		}
		if line.ProductID == 0 && position < len(refs) {
			line.ProductID = refs[position][0]
			line.SKUID = refs[position][1]
		}
		position++
		lines = append(lines, line)
	}
	return lines
}

// stockGroupRefs 返回需要查询库存的商品/SKU：以订单项为准，订单项缺失时回退到已交付行。
func stockGroupRefs(items []orderdomain.OrderItem, lines []deliveredLine) [][2]uint {
	seen := make(map[string]bool)
	groups := make([][2]uint, 0)
	add := func(productID, skuID uint) {
		if productID == 0 {
			return
		}
		key := orderdomain.ItemKey(productID, skuID)
		if seen[key] {
			return
		}
		seen[key] = true
		groups = append(groups, [2]uint{productID, skuID})
	}
	for _, ref := range orderItemRefs(items) {
		add(ref[0], ref[1])
	}
	for _, line := range lines {
		add(line.ProductID, line.SKUID)
	}
	return groups
}

// resolvePairLine 定位配对里的旧卡密所在行：优先用行号，兼容只给 used 卡密 ID 的旧调用方。
func resolvePairLine(pair SecretReplacement, byUsedID map[uint]int) (int, bool) {
	if pair.OldLineIndex >= 0 {
		return pair.OldLineIndex, true
	}
	if pair.OldSecretID != 0 {
		lineIndex, ok := byUsedID[pair.OldSecretID]
		return lineIndex, ok
	}
	return 0, false
}

// ListReplaceCandidates 列出订单已交付的卡密，以及同商品/SKU 下按添加时间从旧到新的可用库存。
func (s *Service) ListReplaceCandidates(orderID uint) (*ReplaceCandidates, error) {
	if orderID == 0 {
		return nil, ErrFulfillmentInvalid
	}
	order, err := s.orderStore.GetByID(orderID)
	if err != nil {
		return nil, ErrOrderFetchFailed
	}
	if order == nil {
		return nil, ErrOrderNotFound
	}
	if order.ParentID == nil && len(order.Children) > 0 {
		return nil, ErrFulfillmentInvalid
	}
	if order.Status != constants.OrderStatusDelivered && order.Status != constants.OrderStatusCompleted {
		return nil, ErrOrderStatusInvalid
	}
	result := &ReplaceCandidates{Delivered: []DeliveredSecret{}, Stock: []StockSecret{}}
	err = s.orderStore.WithinTransaction(func(tx ordercontract.Transaction) error {
		current, found, err := tx.Fulfillments().FindByOrderIDForUpdate(orderID)
		if err != nil {
			return err
		}
		if !found {
			return ErrFulfillmentNotFound
		}
		if current.Type != constants.FulfillmentTypeAuto {
			return ErrFulfillmentNotAuto
		}
		secretRepo := tx.CardSecrets()
		// used 记录可能已被定时清理，这里只是尽力而为：没有记录时按订单项还原规格。
		usedRows, err := secretRepo.ListByOrderAndStatus(orderID, cardsecretdomain.StatusUsed)
		if err != nil {
			return err
		}
		lines := resolveDeliveredLines(current.Payload, order.Items, usedRows)
		for _, line := range lines {
			result.Delivered = append(result.Delivered, DeliveredSecret{
				ID: line.UsedRowID, LineIndex: line.LineIndex, Secret: line.Secret,
				ProductID: line.ProductID, SKUID: line.SKUID, UsedAt: line.UsedAt,
			})
		}
		for _, group := range stockGroupRefs(order.Items, lines) {
			rows, err := secretRepo.ListAvailableByProduct(group[0], group[1], replaceCandidateStockLimit)
			if err != nil {
				return err
			}
			for _, row := range rows {
				result.Stock = append(result.Stock, StockSecret{
					ID: row.ID, Secret: row.Secret, ProductID: row.ProductID, SKUID: row.SKUID, CreatedAt: row.CreatedAt,
				})
			}
		}
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrFulfillmentNotFound), errors.Is(err, ErrFulfillmentNotAuto):
			return nil, err
		default:
			return nil, ErrOrderFetchFailed
		}
	}
	return result, nil
}

// ReplaceSecrets 用库存中的可用卡密替换已交付订单里的指定卡密。
//
// 旧卡密保持 used 状态并保留 order_id，不会被重新售出，也作为替换的追溯依据；
// 新卡密从同商品/SKU 的可用库存中取，库存不足时整体回滚。事务提交后重发订单邮件、
// Telegram 通知，并触发下游回调，让用户拿到新的交付内容。
func (s *Service) ReplaceSecrets(input ReplaceSecretsInput) (*fulfillmentdomain.Fulfillment, int, error) {
	if input.OrderID == 0 || input.AdminID == 0 {
		return nil, 0, ErrFulfillmentInvalid
	}
	order, err := s.orderStore.GetByID(input.OrderID)
	if err != nil {
		return nil, 0, ErrOrderFetchFailed
	}
	if order == nil {
		return nil, 0, ErrOrderNotFound
	}
	if order.ParentID == nil && len(order.Children) > 0 {
		return nil, 0, ErrFulfillmentInvalid
	}
	if order.Status != constants.OrderStatusDelivered && order.Status != constants.OrderStatusCompleted {
		return nil, 0, ErrOrderStatusInvalid
	}

	wanted := make(map[string]int)
	for _, raw := range input.Secrets {
		if text := strings.TrimSpace(raw); text != "" {
			wanted[text]++
		}
	}
	wantAll := len(wanted) == 0

	now := time.Now()
	var updated *fulfillmentdomain.Fulfillment
	replaced := 0
	err = s.orderStore.WithinTransaction(func(tx ordercontract.Transaction) error {
		current, found, err := tx.Fulfillments().FindByOrderIDForUpdate(input.OrderID)
		if err != nil {
			return err
		}
		if !found {
			return ErrFulfillmentNotFound
		}
		if current.Type != constants.FulfillmentTypeAuto {
			return ErrFulfillmentNotAuto
		}

		secretRepo := tx.CardSecrets()
		// used 记录可能已被定时清理，这里只是尽力而为：没有记录时按订单项还原规格。
		usedRows, err := secretRepo.ListByOrderAndStatus(input.OrderID, cardsecretdomain.StatusUsed)
		if err != nil {
			return err
		}

		// 以交付内容本身为准逐行定位，商品/SKU 由行号 + 订单项还原，不再依赖卡密历史。
		lines := strings.Split(current.Payload, "\n")
		delivered := resolveDeliveredLines(current.Payload, order.Items, usedRows)
		type pick struct {
			line    deliveredLine
			lineIdx int
		}
		var picks []pick
		byLine := make(map[int]deliveredLine, len(delivered))
		byUsedID := make(map[uint]int, len(delivered))
		for _, line := range delivered {
			byLine[line.LineIndex] = line
			if line.UsedRowID != 0 {
				byUsedID[line.UsedRowID] = line.LineIndex
			}
		}

		var replacements []cardsecretdomain.Secret
		var newIDs []uint
		if len(input.Pairs) > 0 {
			// 管理员指定「旧卡密行 → 库存新卡密」：旧行必须是本单当前交付内容，新卡密必须仍可用且规格一致。
			seenOld := make(map[int]bool, len(input.Pairs))
			seenNew := make(map[uint]bool, len(input.Pairs))
			wantNewIDs := make([]uint, 0, len(input.Pairs))
			for _, pair := range input.Pairs {
				lineIndex, ok := resolvePairLine(pair, byUsedID)
				if !ok || pair.NewSecretID == 0 {
					return ErrFulfillmentInvalid
				}
				line, ok := byLine[lineIndex]
				if !ok || seenOld[lineIndex] || seenNew[pair.NewSecretID] || line.ProductID == 0 {
					return ErrFulfillmentInvalid
				}
				seenOld[lineIndex] = true
				seenNew[pair.NewSecretID] = true
				picks = append(picks, pick{line: line, lineIdx: lineIndex})
				wantNewIDs = append(wantNewIDs, pair.NewSecretID)
			}
			rows, err := secretRepo.ListByIDs(wantNewIDs)
			if err != nil {
				return err
			}
			byID := make(map[uint]cardsecretdomain.Secret, len(rows))
			for _, row := range rows {
				byID[row.ID] = row
			}
			for i, id := range wantNewIDs {
				row, ok := byID[id]
				if !ok || row.Status != cardsecretdomain.StatusAvailable ||
					row.ProductID != picks[i].line.ProductID || row.SKUID != picks[i].line.SKUID {
					return ErrFulfillmentInvalid
				}
				replacements = append(replacements, row)
				newIDs = append(newIDs, id)
			}
		} else {
			for _, line := range delivered {
				if !wantAll {
					if wanted[line.Secret] <= 0 {
						continue
					}
					wanted[line.Secret]--
				}
				if line.ProductID == 0 {
					return ErrFulfillmentInvalid
				}
				picks = append(picks, pick{line: line, lineIdx: line.LineIndex})
			}
			for _, left := range wanted {
				if left > 0 {
					return ErrFulfillmentInvalid
				}
			}
			if len(picks) == 0 {
				return ErrFulfillmentInvalid
			}

			// 按商品/SKU 分组，从可用库存里取同规格的新卡密（最旧在前）。
			groups := make(map[string][]int)
			var groupOrder []string
			for i, p := range picks {
				key := orderdomain.ItemKey(p.line.ProductID, p.line.SKUID)
				if _, ok := groups[key]; !ok {
					groupOrder = append(groupOrder, key)
				}
				groups[key] = append(groups[key], i)
			}
			replacements = make([]cardsecretdomain.Secret, len(picks))
			newIDs = make([]uint, 0, len(picks))
			for _, key := range groupOrder {
				idxs := groups[key]
				first := picks[idxs[0]].line
				rows, err := secretRepo.ListAvailableByProductForUpdate(first.ProductID, first.SKUID, len(idxs))
				if err != nil {
					return err
				}
				if len(rows) < len(idxs) {
					return ErrCardSecretInsufficient
				}
				for n, pickIdx := range idxs {
					replacements[pickIdx] = rows[n]
					newIDs = append(newIDs, rows[n].ID)
				}
			}
		}
		if len(picks) == 0 {
			return ErrFulfillmentInvalid
		}
		affected, err := secretRepo.MarkUsed(newIDs, input.OrderID, now)
		if err != nil {
			return err
		}
		if int(affected) != len(newIDs) {
			return ErrCardSecretInsufficient
		}

		oldIDs := make([]uint, 0, len(picks))
		oldLineIndexes := make([]int, 0, len(picks))
		for i, p := range picks {
			lines[p.lineIdx] = replacements[i].Secret
			oldLineIndexes = append(oldLineIndexes, p.lineIdx)
			if p.line.UsedRowID != 0 {
				oldIDs = append(oldIDs, p.line.UsedRowID)
			}
		}
		payload := strings.Join(lines, "\n")
		if err := tx.Fulfillments().UpdatePayload(current.ID, payload, now); err != nil {
			return ErrFulfillmentUpdateFailed
		}
		current.Payload = payload
		current.UpdatedAt = now
		updated = current
		replaced = len(picks)
		logger.Infow("fulfillment_secrets_replaced",
			"order_id", input.OrderID,
			"admin_id", input.AdminID,
			"reason", strings.TrimSpace(input.Reason),
			"old_line_indexes", oldLineIndexes,
			"old_secret_ids", oldIDs,
			"new_secret_ids", newIDs,
		)
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrFulfillmentNotFound),
			errors.Is(err, ErrFulfillmentNotAuto),
			errors.Is(err, ErrFulfillmentInvalid),
			errors.Is(err, ErrCardSecretInsufficient),
			errors.Is(err, ErrFulfillmentUpdateFailed):
			return nil, 0, err
		default:
			return nil, 0, ErrFulfillmentUpdateFailed
		}
	}

	// 提交后重新推送：邮件带上新的交付内容，Telegram 与下游站点同步通知。
	notifyOrderID := input.OrderID
	emailStatus := order.Status
	if order.ParentID != nil {
		notifyOrderID = *order.ParentID
		if parent, parentErr := s.orderStore.GetByID(*order.ParentID); parentErr == nil && parent != nil {
			emailStatus = parent.Status
		}
	}
	if s.orderQueue != nil {
		emailQueue := s.orderQueue
		if replacedQueue, ok := s.orderQueue.(replacedStatusEmailQueue); ok {
			emailQueue = replacedEmailQueue{Queue: s.orderQueue, replaced: replacedQueue}
		}
		if _, err := orderapp.EnqueueStatusEmailTaskIfEligible(s.orderStore, emailQueue, s.settingService, s.defaultEmailConfig, notifyOrderID, emailStatus); err != nil {
			logger.Warnw("fulfillment_enqueue_status_email_failed",
				"order_id", order.ID,
				"target_order_id", notifyOrderID,
				"status", emailStatus,
				"error", err,
			)
		}
	}
	go s.notifyBotOrder(order.UserID, notifyOrderID, true)
	if s.downstreamCallbackSvc != nil {
		s.downstreamCallbackSvc.EnqueueCallback(input.OrderID)
	}
	return updated, replaced, nil
}

// NotifyBotOrderFulfilled 查找用户 Telegram 绑定并入队通知任务。
func (s *Service) NotifyBotOrderFulfilled(userID, orderID uint) {
	s.notifyBotOrder(userID, orderID, false)
}

// notifyBotOrder 查找用户 Telegram 绑定并入队通知；replaced 为 true 时表示管理员替换卡密后的重新推送。
func (s *Service) notifyBotOrder(userID, orderID uint, replaced bool) {
	if s.botNotifier == nil || userID == 0 || s.userOAuthIdentityRepo == nil {
		return
	}

	identity, err := s.userOAuthIdentityRepo.GetByUserProvider(userID, "telegram")
	if err != nil {
		logger.Warnw("fulfillment_notify_bot_fetch_identity_failed",
			"order_id", orderID, "user_id", userID, "error", err)
		return
	}
	if identity == nil || strings.TrimSpace(identity.ProviderUserID) == "" {
		return
	}

	telegramUserID := strings.TrimSpace(identity.ProviderUserID)
	var enqueueErr error
	if replaced {
		enqueueErr = s.botNotifier.EnqueueOrderReplaced(telegramUserID, orderID)
	} else {
		enqueueErr = s.botNotifier.EnqueueOrderFulfilled(telegramUserID, orderID)
	}
	if err := enqueueErr; err != nil {
		logger.Warnw("fulfillment_notify_bot_enqueue_failed",
			"order_id", orderID, "user_id", userID, "error", err)
	}
}

func normalizeManualDeliveryData(raw jsonmap.JSON) jsonmap.JSON {
	if len(raw) == 0 {
		return jsonmap.JSON{}
	}
	normalized := jsonmap.JSON{}
	note := strings.TrimSpace(toStringValue(raw["note"]))
	if note != "" {
		normalized["note"] = note
	}
	if entriesRaw, ok := raw["entries"]; ok {
		entries := normalizeManualDeliveryEntries(entriesRaw)
		if len(entries) > 0 {
			normalized["entries"] = entries
		}
	}

	keys := make([]string, 0, len(raw))
	for key := range raw {
		if key == "note" || key == "entries" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := normalizeManualDeliveryPrimitive(raw[key])
		if value == nil {
			continue
		}
		normalized[key] = value
	}

	return normalized
}

func normalizeManualDeliveryEntries(raw interface{}) []jsonmap.JSON {
	appendEntry := func(entries []jsonmap.JSON, row map[string]interface{}) []jsonmap.JSON {
		key := strings.TrimSpace(toStringValue(row["key"]))
		value := strings.TrimSpace(toStringValue(row["value"]))
		if key == "" && value == "" {
			return entries
		}
		entry := jsonmap.JSON{}
		if key != "" {
			entry["key"] = key
		}
		if value != "" {
			entry["value"] = value
		}
		return append(entries, entry)
	}

	entries := make([]jsonmap.JSON, 0)
	switch value := raw.(type) {
	case []jsonmap.JSON:
		for _, item := range value {
			entries = appendEntry(entries, item)
		}
		return entries
	case []map[string]interface{}:
		for _, item := range value {
			entries = appendEntry(entries, item)
		}
		return entries
	case []interface{}:
		for _, item := range value {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			entries = appendEntry(entries, row)
		}
		return entries
	default:
		return nil
	}
}

func normalizeManualDeliveryPrimitive(raw interface{}) interface{} {
	switch value := raw.(type) {
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return nil
		}
		return text
	case bool:
		return value
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return value
	default:
		return nil
	}
}

func buildManualDeliveryPayload(data jsonmap.JSON) string {
	if len(data) == 0 {
		return ""
	}
	lines := make([]string, 0, 8)
	if note, ok := data["note"].(string); ok && strings.TrimSpace(note) != "" {
		lines = append(lines, note)
	}
	if entries, ok := data["entries"].([]jsonmap.JSON); ok {
		for _, item := range entries {
			key := strings.TrimSpace(toStringValue(item["key"]))
			value := strings.TrimSpace(toStringValue(item["value"]))
			if key == "" && value == "" {
				continue
			}
			if key == "" {
				lines = append(lines, value)
				continue
			}
			if value == "" {
				lines = append(lines, key)
				continue
			}
			lines = append(lines, fmt.Sprintf("%s: %s", key, value))
		}
	}

	extraKeys := make([]string, 0, len(data))
	for key := range data {
		if key == "note" || key == "entries" {
			continue
		}
		extraKeys = append(extraKeys, key)
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		valueText := strings.TrimSpace(toStringValue(data[key]))
		if valueText == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", key, valueText))
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func toStringValue(raw interface{}) string {
	switch value := raw.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, bool:
		return fmt.Sprintf("%v", value)
	default:
		return ""
	}
}
