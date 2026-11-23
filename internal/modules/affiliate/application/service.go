package application

import (
	"time"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	affiliatecontract "github.com/dujiao-next/internal/modules/affiliate/contract"
	usercontract "github.com/dujiao-next/internal/modules/identity/user/contract"
)

const (
	affiliateCodeLength        = 8
	affiliateSplitTypePrefix   = "sp"
	affiliateAttributionWindow = 30 * 24 * time.Hour
	affiliateClickDedupeWindow = 10 * time.Minute
)

// WithdrawNotifier 在提现申请创建后投递通知。
// 由 SetWithdrawNotifier 延迟注入，解决容器装配顺序依赖
// （NotificationService 构建晚于本服务）。
//
// 参数用基本类型而不是结构体：reseller 与 affiliate 模块各自声明该接口，
// 同一份结构体会因包路径不同而无法被同一个实现同时满足。
type WithdrawNotifier interface {
	NotifyWithdrawSubmitted(scope string, requestID, userID uint, amount, currency, channel, account string)
}

// ScopeAffiliate 推广返利提现申请的类别标识。
const ScopeAffiliate = "affiliate"

// Service 推广返利业务服务
type Service struct {
	repo        affiliatecontract.Store
	userRepo    usercontract.Store
	orderRepo   affiliatecontract.OrderReader
	productRepo affiliatecontract.ProductReader
	skuRepo     affiliatecontract.SKUReader
	settings    affiliatecontract.SettingsReader
	notifier    WithdrawNotifier
}

// SetWithdrawNotifier 注入提现通知依赖（在容器构建 NotificationService 之后调用）。
func (s *Service) SetWithdrawNotifier(notifier WithdrawNotifier) {
	if s == nil {
		return
	}
	s.notifier = notifier
}

// noopSKUReader 在调用方没有提供 SKU 读取器时的空实现。
// 用它兜底而不是在各处判 nil，避免新增构造点时漏注入导致 SKU 级比例被静默忽略。
type noopSKUReader struct{}

func (noopSKUReader) ListByIDs([]uint) ([]productdomain.ProductSKU, error) {
	return nil, nil
}

func (noopSKUReader) ListByProduct(uint, bool) ([]productdomain.ProductSKU, error) {
	return nil, nil
}

// NewService 创建推广返利服务
func NewService(
	repo affiliatecontract.Store,
	userRepo usercontract.Store,
	orderRepo affiliatecontract.OrderReader,
	productRepo affiliatecontract.ProductReader,
	skuRepo affiliatecontract.SKUReader,
	settings affiliatecontract.SettingsReader,
) *Service {
	if skuRepo == nil {
		skuRepo = noopSKUReader{}
	}
	return &Service{
		repo:        repo,
		userRepo:    userRepo,
		orderRepo:   orderRepo,
		productRepo: productRepo,
		skuRepo:     skuRepo,
		settings:    settings,
	}
}
