package errorloghttp

import "github.com/gin-gonic/gin"

// RegisterAdminRoutes 注册管理端错误日志路由。
func RegisterAdminRoutes(admin gin.IRoutes, handler *AdminHandler) {
	admin.GET("/error-logs", handler.List)
}
