package acg

import "github.com/gin-gonic/gin"

// RegisterRoutes 挂载 acg-faka 入站供货路由。
// 调用方（app 层）负责分组前缀与中间件，本包只做端点绑定，
// 避免 transport 反向依赖 app/httpserver/middleware。
func RegisterRoutes(shared gin.IRoutes, handler *Handler) {
	if shared == nil || handler == nil {
		panic("acg supply routes: required dependency is nil")
	}
	shared.POST("/authentication/connect", handler.Connect)
	shared.POST("/commodity/items", handler.Items)
	shared.POST("/commodity/item", handler.Item)
	shared.POST("/commodity/stock", handler.Stock)
	shared.POST("/commodity/valuation", handler.Valuation)
	shared.POST("/commodity/trade", handler.Trade)
	shared.POST("/commodity/query", handler.Query)
	// 旧版 acg / MCYShopV3 插件沿用这两组端点名。
	shared.POST("/commodity/inventory", handler.Inventory)
	shared.POST("/commodity/inventoryState", handler.InventoryState)
}
