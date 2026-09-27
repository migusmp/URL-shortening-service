package server

import (
	"github.com/gin-gonic/gin"
	"github.com/migus/url_shortener/internal/config"
	"github.com/migus/url_shortener/internal/handler"
	"github.com/migus/url_shortener/internal/middleware"
)

// Dependencies holds everything the HTTP router needs.
type Dependencies struct {
	Config      *config.Config
	Handler     *handler.URLHandler
	RateLimiter middleware.RateLimiter
}

// NewRouter builds the Gin engine and registers all routes.
func NewRouter(deps Dependencies) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())

	registerHealthRoutes(r, deps.Handler)
	registerAPIRoutes(r, deps)
	registerRedirectRoutes(r, deps)

	return r
}

func registerHealthRoutes(r *gin.Engine, h *handler.URLHandler) {
	r.GET("/health", h.Health)
}

func registerAPIRoutes(r *gin.Engine, deps Dependencies) {
	cfg := deps.Config
	windowSecs := rateWindowSeconds(cfg)

	api := r.Group("/api/v1")
	api.Use(middleware.APIKeyAuth(cfg.APIKeys))
	api.Use(middleware.RateLimit(middleware.RateLimitConfig{
		Limiter:    deps.RateLimiter,
		Limit:      cfg.RateLimitAPI,
		WindowSecs: windowSecs,
		KeyFunc:    middleware.APIKeyRateKey("api"),
	}))

	h := deps.Handler
	api.POST("/urls", h.Create)
	api.GET("/urls/:code", h.Get)
	api.PUT("/urls/:code", h.Update)
	api.DELETE("/urls/:code", h.Delete)
}

func registerRedirectRoutes(r *gin.Engine, deps Dependencies) {
	cfg := deps.Config
	windowSecs := rateWindowSeconds(cfg)

	r.GET("/:code",
		middleware.RateLimit(middleware.RateLimitConfig{
			Limiter:    deps.RateLimiter,
			Limit:      cfg.RateLimitRedirect,
			WindowSecs: windowSecs,
			KeyFunc:    middleware.ClientIPKey("redirect"),
		}),
		deps.Handler.Redirect,
	)
}

func rateWindowSeconds(cfg *config.Config) int {
	secs := int(cfg.RateLimitWindow.Seconds())
	if secs < 1 {
		return 60
	}
	return secs
}
