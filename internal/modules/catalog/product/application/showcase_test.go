package productapplication

import (
	"errors"
	"reflect"
	"testing"

	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
)

type showcaseProductRepositoryStub struct {
	rowsByCategory map[uint][]productdomain.Product
	filters        []productcontract.ListFilter
}

func (stub *showcaseProductRepositoryStub) List(filter productcontract.ListFilter) ([]productdomain.Product, int64, error) {
	stub.filters = append(stub.filters, filter)
	rows := make([]productdomain.Product, 0)
	for _, categoryID := range filter.CategoryIDs {
		rows = append(rows, stub.rowsByCategory[categoryID]...)
	}
	if filter.PageSize > 0 && len(rows) > filter.PageSize {
		rows = rows[:filter.PageSize]
	}
	return rows, int64(len(rows)), nil
}

func (stub *showcaseProductRepositoryStub) GetBySlug(string, bool) (*productdomain.Product, error) {
	return nil, nil
}

func (stub *showcaseProductRepositoryStub) GetAdminByID(string) (*productdomain.Product, error) {
	return nil, nil
}

func showcaseProduct(id uint, categoryID uint) productdomain.Product {
	return productdomain.Product{ID: id, CategoryID: categoryID, IsActive: true}
}

func TestListShowcaseLimitsPerCategoryAndMergesChildren(t *testing.T) {
	categories := categoryRepositoryStub{rows: []categorydomain.Category{
		{ID: 10, IsActive: true},
		{ID: 20, IsActive: true},
		{ID: 11, ParentID: 10, IsActive: true},
		{ID: 21, ParentID: 20, IsActive: true},
		{ID: 30, IsActive: false},
		{ID: 31, ParentID: 30, IsActive: true},
	}}
	products := &showcaseProductRepositoryStub{rowsByCategory: map[uint][]productdomain.Product{
		11: {showcaseProduct(111, 11), showcaseProduct(112, 11), showcaseProduct(113, 11)},
		20: {showcaseProduct(201, 20)},
		21: {showcaseProduct(211, 21)},
	}}
	service := NewService(Options{Products: products, Categories: categories})

	result, err := service.ListShowcase(2, 10)
	if err != nil {
		t.Fatalf("ListShowcase returned error: %v", err)
	}

	wantIDs := []uint{111, 112, 201, 211}
	gotIDs := make([]uint, 0, len(result))
	for _, item := range result {
		gotIDs = append(gotIDs, item.ID)
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("showcase ids want %v got %v", wantIDs, gotIDs)
	}

	wantFilters := [][]uint{{10, 11}, {20, 21}}
	if len(products.filters) != len(wantFilters) {
		t.Fatalf("want %d category queries, got %d", len(wantFilters), len(products.filters))
	}
	for i, want := range wantFilters {
		filter := products.filters[i]
		if !reflect.DeepEqual(filter.CategoryIDs, want) {
			t.Fatalf("filter %d category ids want %v got %v", i, want, filter.CategoryIDs)
		}
		if filter.Page != 1 || filter.PageSize != 2 || !filter.OnlyActive || !filter.WithCategory {
			t.Fatalf("filter %d unexpected: %#v", i, filter)
		}
	}
}

func TestListShowcaseAppliesDefaultsAndCaps(t *testing.T) {
	categories := categoryRepositoryStub{rows: []categorydomain.Category{
		{ID: 10, IsActive: true},
		{ID: 20, IsActive: true},
	}}

	products := &showcaseProductRepositoryStub{rowsByCategory: map[uint][]productdomain.Product{
		10: {showcaseProduct(101, 10)},
		20: {showcaseProduct(201, 20)},
	}}
	service := NewService(Options{Products: products, Categories: categories})
	if _, err := service.ListShowcase(0, 0); err != nil {
		t.Fatalf("ListShowcase returned error: %v", err)
	}
	if got := products.filters[0].PageSize; got != DefaultShowcasePerCategory {
		t.Fatalf("default per category want %d got %d", DefaultShowcasePerCategory, got)
	}

	products = &showcaseProductRepositoryStub{rowsByCategory: map[uint][]productdomain.Product{
		10: {showcaseProduct(101, 10)},
		20: {showcaseProduct(201, 20)},
	}}
	service = NewService(Options{Products: products, Categories: categories})
	if _, err := service.ListShowcase(MaxShowcasePerCategory+10, 1); err != nil {
		t.Fatalf("ListShowcase returned error: %v", err)
	}
	if got := products.filters[0].PageSize; got != MaxShowcasePerCategory {
		t.Fatalf("capped per category want %d got %d", MaxShowcasePerCategory, got)
	}
	if len(products.filters) != 1 {
		t.Fatalf("max categories 1 should stop after first non-empty group, got %d queries", len(products.filters))
	}
}

func TestListShowcaseSkipsEmptyCategoriesAndInactiveOnes(t *testing.T) {
	categories := categoryRepositoryStub{rows: []categorydomain.Category{
		{ID: 10, IsActive: true},
		{ID: 20, IsActive: true},
		{ID: 30, IsActive: false},
	}}
	products := &showcaseProductRepositoryStub{rowsByCategory: map[uint][]productdomain.Product{
		20: {showcaseProduct(201, 20)},
		30: {showcaseProduct(301, 30)},
	}}
	service := NewService(Options{Products: products, Categories: categories})

	result, err := service.ListShowcase(4, 10)
	if err != nil {
		t.Fatalf("ListShowcase returned error: %v", err)
	}
	if len(result) != 1 || result[0].ID != 201 {
		t.Fatalf("want only active category 20 product, got %+v", result)
	}
	for _, filter := range products.filters {
		if containsUint(filter.CategoryIDs, 30) {
			t.Fatalf("inactive category 30 must not be queried: %#v", filter)
		}
	}
}

func TestListShowcaseForTenantExcludesHiddenProducts(t *testing.T) {
	categories := categoryRepositoryStub{rows: []categorydomain.Category{{ID: 10, IsActive: true}}}
	products := &showcaseProductRepositoryStub{rowsByCategory: map[uint][]productdomain.Product{
		10: {showcaseProduct(101, 10)},
	}}
	service := NewService(Options{Products: products, Categories: categories})
	resellerID := uint(9)

	if _, err := service.ListShowcaseForTenant(resellercontract.TenantContext{ResellerID: &resellerID}, nil, 4, 10); !errors.Is(err, productcontract.ErrResellerProductNotListed) {
		t.Fatalf("want not-listed error without hidden repository, got %v", err)
	}

	if _, err := service.ListShowcaseForTenant(
		resellercontract.TenantContext{ResellerID: &resellerID},
		hiddenProductRepositoryStub{ids: []uint{7, 8}},
		4,
		10,
	); err != nil {
		t.Fatalf("ListShowcaseForTenant returned error: %v", err)
	}
	if !reflect.DeepEqual(products.filters[0].ExcludeProductIDs, []uint{7, 8}) {
		t.Fatalf("excluded ids want [7 8] got %v", products.filters[0].ExcludeProductIDs)
	}
}

func containsUint(values []uint, target uint) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
