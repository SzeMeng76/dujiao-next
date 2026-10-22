package seoguidehttp

import "github.com/gin-gonic/gin"

// RegisterRoutes 注册 /guide 系列路由。
func RegisterRoutes(engine gin.IRoutes, handler *Handler) {
	engine.GET("/guide", handler.GetGuideHome)
	engine.GET("/guide/c/:slug", handler.GetGuideCategory)
}
