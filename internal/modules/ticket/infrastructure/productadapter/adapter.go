package productadapter

import (
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// Adapter 把商品仓储适配为工单模块所需的商品标题解析端口。
type Adapter struct {
	products productcontract.Repository
}

// New 创建商品标题解析适配器。
func New(products productcontract.Repository) *Adapter {
	return &Adapter{products: products}
}

// ResolveProductTitles 按商品ID批量解析多语言标题（raw JSON），仅用于工单响应展示。
// 商品已被删除时该 ID 不会出现在结果中，前端按"商品已删除"处理。
func (a *Adapter) ResolveProductTitles(ids []uint) (map[uint]jsonmap.JSON, error) {
	if a.products == nil || len(ids) == 0 {
		return nil, nil
	}
	products, err := a.products.ListByIDs(ids)
	if err != nil {
		return nil, err
	}
	result := make(map[uint]jsonmap.JSON, len(products))
	for _, product := range products {
		if len(product.TitleJSON) == 0 {
			continue
		}
		result[product.ID] = product.TitleJSON
	}
	return result, nil
}
