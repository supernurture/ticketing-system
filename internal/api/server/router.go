package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"ticketing-system/internal/api/server/modules/auth"
	"ticketing-system/internal/api/server/modules/health"
	"ticketing-system/internal/api/server/modules/showtime"
	authcontract "ticketing-system/internal/api/server/oapicodegen/auth"
	healthcontract "ticketing-system/internal/api/server/oapicodegen/health"
	showtimecontract "ticketing-system/internal/api/server/oapicodegen/showtime"
	"ticketing-system/internal/config"
	"ticketing-system/internal/container"
	"ticketing-system/internal/middleware"
)

// NewRouter builds the gin engine with the middleware chain and every module's routes.
func NewRouter(cfg *config.Config, deps *container.Container) (*gin.Engine, error) {
	gin.SetMode(cfg.Server.Mode)

	router := gin.New()
	if err := router.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}

	// Lets the request deadline reach handlers and downstream calls.
	router.ContextWithFallback = true
	router.Use(middleware.Default(cfg, deps.Logger)...)

	register(router, cfg, deps)
	return router, nil
}

func register(router gin.IRouter, cfg *config.Config, deps *container.Container) {
	healthcontract.RegisterHandlersWithOptions(router,
		healthcontract.NewStrictHandlerWithOptions(health.NewHandler(), nil, healthOptions),
		healthcontract.GinServerOptions{ErrorHandler: invalidParam})

	db := deps.Postgres["ticketing"]
	if db == nil {
		return
	}
	secret := []byte(cfg.Auth.JWTSecret)

	authcontract.RegisterHandlersWithOptions(router,
		authcontract.NewStrictHandlerWithOptions(
			auth.NewHandler(auth.NewService(auth.NewRepository(db), secret, cfg.Auth.TokenTTL, deps.Logger)),
			nil, authOptions),
		authcontract.GinServerOptions{ErrorHandler: invalidParam})

	// Auth runs before parameter parsing, so anonymous callers always get 401.
	showtimecontract.RegisterHandlersWithOptions(router.Group("", middleware.Auth(secret)),
		showtimecontract.NewStrictHandlerWithOptions(
			showtime.NewHandler(showtime.NewService(showtime.NewRepository(db)), deps.Logger), nil, showtimeOptions),
		showtimecontract.GinServerOptions{ErrorHandler: invalidParam})
}

// Replace the generated defaults, which leak err.Error() and answer "msg" instead of "message".
var (
	healthOptions = healthcontract.StrictGinServerOptions{
		RequestErrorHandlerFunc: badRequest, HandlerErrorFunc: internalError, ResponseErrorHandlerFunc: internalError,
	}
	authOptions = authcontract.StrictGinServerOptions{
		RequestErrorHandlerFunc: badRequest, HandlerErrorFunc: internalError, ResponseErrorHandlerFunc: internalError,
	}
	showtimeOptions = showtimecontract.StrictGinServerOptions{
		RequestErrorHandlerFunc: badRequest, HandlerErrorFunc: internalError, ResponseErrorHandlerFunc: internalError,
	}
)

// invalidParam answers an unparsable path or query parameter.
func invalidParam(c *gin.Context, err error, status int) {
	_ = c.Error(err)
	c.JSON(status, gin.H{"message": err.Error()})
}

// badRequest answers an undecodable request body.
func badRequest(c *gin.Context, err error) {
	_ = c.Error(err)
	switch tooLarge := (*http.MaxBytesError)(nil); {
	case errors.As(err, &tooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"message": "request body too large"})
	case errors.Is(err, io.EOF):
		c.JSON(http.StatusBadRequest, gin.H{"message": "a JSON body is required"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	}
}

// internalError keeps the cause in c.Errors for AccessLog and out of the response.
func internalError(c *gin.Context, err error) {
	_ = c.Error(err)
	// No body past the deadline (Timeout answers 504) or after a failed write.
	if c.Writer.Written() || c.Request.Context().Err() != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"message": "internal server error"})
}
