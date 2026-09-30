package tickethttp

import "github.com/gin-gonic/gin"

// RegisterUserRoutes 注册前台用户工单路由。
func RegisterUserRoutes(user gin.IRoutes, handler *UserHandler) {
	if user == nil || handler == nil {
		panic("ticket user routes: required dependency is nil")
	}
	user.GET("/tickets", handler.ListTickets)
	user.POST("/tickets", handler.CreateTicket)
	user.GET("/tickets/badge", handler.Badge)
	user.POST("/tickets/upload", handler.UploadImage)
	user.GET("/tickets/:id", handler.GetTicket)
	user.POST("/tickets/:id/reply", handler.ReplyTicket)
}

// RegisterAdminRoutes 注册后台工单路由。
func RegisterAdminRoutes(authorized gin.IRoutes, handler *AdminHandler) {
	if authorized == nil || handler == nil {
		panic("ticket admin routes: required dependency is nil")
	}
	authorized.GET("/tickets", handler.ListTickets)
	authorized.GET("/tickets/badge", handler.Badge)
	authorized.GET("/tickets/:id", handler.GetTicket)
	authorized.POST("/tickets/:id/reply", handler.ReplyTicket)
	authorized.POST("/tickets/:id/close", handler.CloseTicket)
}
