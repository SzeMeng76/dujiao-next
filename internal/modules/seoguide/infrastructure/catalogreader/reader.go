package seoguidecatalogreader

import (
	categorycontract "github.com/dujiao-next/internal/modules/catalog/category/contract"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	seoguidecontract "github.com/dujiao-next/internal/modules/seoguide/contract"
)

// Reader 将 Catalog 仓储适配为 seoguide 所需的 CatalogReader 端口。
type Reader struct {
	products   productcontract.Repository
	categories categorycontract.Repository
}

var _ seoguidecontract.CatalogReader = (*Reader)(nil)

// New 创建适配器。
func New(products productcontract.Repository, categories categorycontract.Repository) *Reader {
	if products == nil || categories == nil {
		panic("seoguide catalog reader: required repository is nil")
	}
	return &Reader{products: products, categories: categories}
}

// ListActiveCategories 返回所有启用的分类投影。
func (r *Reader) ListActiveCategories() ([]seoguidecontract.Category, error) {
	rows, err := r.categories.ListActive()
	if err != nil {
		return nil, err
	}
	result := make([]seoguidecontract.Category, 0, len(rows))
	for _, row := range rows {
		result = append(result, seoguidecontract.Category{
			ID:        row.ID,
			Slug:      row.Slug,
			Name:      map[string]interface{}(row.NameJSON),
			SortOrder: row.SortOrder,
		})
	}
	return result, nil
}

// ListActiveProducts 返回所有上架商品的 SEO 投影（前 limit 条）。
func (r *Reader) ListActiveProducts(limit int) ([]seoguidecontract.Product, error) {
	rows, _, err := r.products.List(productcontract.ListFilter{
		Page:       1,
		PageSize:   limit,
		OnlyActive: true,
	})
	if err != nil {
		return nil, err
	}
	return projectProducts(rows), nil
}

// ListActiveProductsByCategory 返回指定分类下所有上架商品。
func (r *Reader) ListActiveProductsByCategory(categoryID uint, limit int) ([]seoguidecontract.Product, error) {
	rows, _, err := r.products.List(productcontract.ListFilter{
		Page:        1,
		PageSize:    limit,
		CategoryIDs: []uint{categoryID},
		OnlyActive:  true,
	})
	if err != nil {
		return nil, err
	}
	return projectProducts(rows), nil
}

func projectProducts(rows []productdomain.Product) []seoguidecontract.Product {
	result := make([]seoguidecontract.Product, 0, len(rows))
	for _, row := range rows {
		result = append(result, seoguidecontract.Product{
			Slug:        row.Slug,
			Title:       map[string]interface{}(row.TitleJSON),
			Description: map[string]interface{}(row.DescriptionJSON),
			SeoMeta:     map[string]interface{}(row.SeoMetaJSON),
			PriceAmount: row.PriceAmount.String(),
			Images:      []string(row.Images),
			CategoryID:  row.CategoryID,
			UpdatedAt:   row.UpdatedAt,
		})
	}
	return result
}
