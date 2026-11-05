// Package httpapi 是服务端唯一的 HTTP 边界。
//
// 路由、只读策略、错误到状态码的映射都集中在这里，
// 业务规则留在 service，机台访问留在 machine。
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"recipeworkbench/apps/server/internal/service"
)

// readPolicy 是首期的只读策略：任何主体只能对 /api/* 发起 GET。
const readPolicy = `[request_definition]
r = sub, obj, act
[policy_definition]
p = sub, obj, act
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = r.sub == p.sub && keyMatch2(r.obj, p.obj) && r.act == p.act
`

// NewRouter 组装路由与中间件。
func NewRouter(svc *service.Service, logger *slog.Logger) (*gin.Engine, error) {
	policy, err := model.NewModelFromString(readPolicy)
	if err != nil {
		return nil, err
	}
	enforcer, err := casbin.NewEnforcer(policy)
	if err != nil {
		return nil, err
	}
	if _, err := enforcer.AddPolicy("local-reader", "/api/*", "GET"); err != nil {
		return nil, err
	}

	router := gin.New()
	router.Use(RequestLogger(logger), gin.Recovery())
	router.HandleMethodNotAllowed = true
	router.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "首期仅提供读取接口"})
	})
	router.NoRoute(func(c *gin.Context) { c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"}) })
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	handlers := &handlers{svc: svc}
	api := router.Group("/api")
	api.Use(func(c *gin.Context) {
		allowed, err := enforcer.Enforce("local-reader", c.Request.URL.Path, c.Request.Method)
		if err != nil || !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无读取权限"})
			return
		}
		c.Next()
	})
	api.GET("/machines", handlers.listMachines)
	api.GET("/machines/:machine/recipes", handlers.listRecipes)
	api.GET("/machines/:machine/recipes/:recipe", handlers.getRecipe)
	api.GET("/machines/:machine/recipes/:recipe/source", handlers.downloadSource)
	return router, nil
}
