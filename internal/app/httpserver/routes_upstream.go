package httpserver

import (
	"github.com/dujiao-next/internal/app/container"
	"github.com/dujiao-next/internal/app/httpserver/middleware"
	upstreamtransport "github.com/dujiao-next/internal/modules/upstreamapi/transport/http"
	upstreamacg "github.com/dujiao-next/internal/modules/upstreamapi/transport/http/acg"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func registerUpstreamRoutes(
	r *gin.Engine,
	apiV1 *gin.RouterGroup,
	c *container.Container,
	upstreamHandler *upstreamtransport.Handler,
	redisClient *redis.Client,
	upstreamAPIRule middleware.RateLimitRule,
	callbackRule middleware.RateLimitRule,
) {
	// 上游 API（本站作为 B 站点，暴露给下游 A 调用）
	upstreamAPI := apiV1.Group("/upstream")
	upstreamAPI.Use(middleware.RateLimitMiddleware(redisClient, upstreamAPIRule, middleware.KeyByUpstreamApiKey))
	upstreamAPI.Use(middleware.UpstreamAPIAuthMiddleware(c.ApiCredentialRepo))
	upstreamtransport.RegisterAuthenticatedRoutes(upstreamAPI, upstreamHandler)

	// 上游回调接收（本站作为 A 站点，接收 B 的回调）
	apiV1.POST("/upstream/callback", middleware.RateLimitMiddleware(redisClient, callbackRule, middleware.KeyByIP), upstreamHandler.HandleCallback)

	// acg-faka 入站供货接口（本站作为供货方，暴露给 acg 系下游）。
	// 独立前缀 /shared + 独立验签，与上方 /api/v1/upstream 完全隔离：
	// 同一份 api_credentials 两种协议都能识别，但不共享路由与中间件。
	acgSupply := upstreamacg.New(upstreamHandler, c.ApiCredentialRepo, c.FulfillmentService)
	shared := r.Group("/shared")
	shared.Use(middleware.RateLimitMiddleware(redisClient, upstreamAPIRule, middleware.KeyByIP))
	upstreamacg.RegisterRoutes(shared, acgSupply)
}
