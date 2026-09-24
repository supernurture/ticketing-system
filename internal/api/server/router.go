package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"ticketing-system/internal/api/server/modules/health"
	healthcontract "ticketing-system/internal/api/server/oapicodegen/health"
	"ticketing-system/internal/config"
	"ticketing-system/internal/container"
	"ticketing-system/internal/middleware"
)

// NewRouter builds the gin engine: mode, trusted proxies, the middleware chain, and every module's generated routes.
func NewRouter(cfg *config.Config, deps *container.Container) (*gin.Engine, error) {
	gin.SetMode(cfg.Server.Mode)

	router := gin.New()
	if err := router.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}

	// Without this, a handler's *gin.Context carries no deadline and the timeout never reaches downstream calls.
	router.ContextWithFallback = true
	router.Use(middleware.Default(cfg, deps.Logger)...)

	register(router)
	return router, nil
}

func register(router gin.IRouter) {
	healthcontract.RegisterHandlersWithOptions(router,
		healthcontract.NewStrictHandlerWithOptions(health.NewHandler(), nil, healthOptions),
		healthcontract.GinServerOptions{ErrorHandler: invalidParam})
}

// The generated defaults write err.Error() into the body, leaking internals such as database errors,
// and answer {"msg": ...} where the spec's Error, Recovery and Timeout all use "message".
var (
	healthOptions = healthcontract.StrictGinServerOptions{
		RequestErrorHandlerFunc: badRequest, HandlerErrorFunc: internalError, ResponseErrorHandlerFunc: internalError,
	}
)

// invalidParam answers a path or query parameter that does not parse; the message names the parameter.
func invalidParam(c *gin.Context, err error, status int) {
	_ = c.Error(err)
	c.JSON(status, gin.H{"message": err.Error()})
}

// badRequest answers a body the server could not decode; the decoder's message is about the caller's input.
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
	// Left unwritten past the deadline, so Timeout can answer 504; and a response already
	// on the wire (a failed write) must not get a second body appended.
	if c.Writer.Written() || c.Request.Context().Err() != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"message": "internal server error"})
}
