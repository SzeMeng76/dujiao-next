package application

import (
	"testing"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestBuildOrderSubject(t *testing.T) {
	tests := []struct {
		name  string
		order *orderdomain.Order
		want  string
	}{
		{
			name: "nil order",
			want: "",
		},
		{
			name: "legacy parent item title",
			order: &orderdomain.Order{
				OrderNo: "DJ-LEGACY-1",
				Items: []orderdomain.OrderItem{
					{TitleJSON: jsonmap.JSON{"zh-CN": "父订单商品"}},
				},
			},
			want: "父订单商品",
		},
		{
			name: "current child item title",
			order: &orderdomain.Order{
				OrderNo: "DJ-CURRENT-1",
				Children: []orderdomain.Order{
					{
						Items: []orderdomain.OrderItem{
							{TitleJSON: jsonmap.JSON{"zh-CN": "子订单商品"}},
						},
					},
				},
			},
			want: "子订单商品",
		},
		{
			name: "first non-empty item title",
			order: &orderdomain.Order{
				OrderNo: "DJ-CURRENT-2",
				Children: []orderdomain.Order{
					{Items: []orderdomain.OrderItem{{TitleJSON: jsonmap.JSON{"zh-CN": " "}}}},
					{Items: []orderdomain.OrderItem{{TitleJSON: jsonmap.JSON{"en-US": "Second product"}}}},
				},
			},
			want: "Second product",
		},
		{
			name: "parent item takes precedence",
			order: &orderdomain.Order{
				OrderNo: "DJ-MIXED-1",
				Items: []orderdomain.OrderItem{
					{TitleJSON: jsonmap.JSON{"zh-CN": "父订单商品"}},
				},
				Children: []orderdomain.Order{
					{Items: []orderdomain.OrderItem{{TitleJSON: jsonmap.JSON{"zh-CN": "子订单商品"}}}},
				},
			},
			want: "父订单商品",
		},
		{
			name: "order number fallback",
			order: &orderdomain.Order{
				OrderNo: " DJ-FALLBACK-1 ",
				Children: []orderdomain.Order{
					{Items: []orderdomain.OrderItem{{TitleJSON: jsonmap.JSON{}}}},
				},
			},
			want: "DJ-FALLBACK-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildOrderSubject(tt.order); got != tt.want {
				t.Fatalf("buildOrderSubject() = %q, want %q", got, tt.want)
			}
		})
	}
}
