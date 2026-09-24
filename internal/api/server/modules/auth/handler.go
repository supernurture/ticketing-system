package auth

import (
	"context"
	"errors"
	"strings"

	authcontract "ticketing-system/internal/api/server/oapicodegen/auth"
	"ticketing-system/internal/middleware"
	"ticketing-system/pkg/logger"
)

type Handler struct {
	service *Service
	log     *logger.Logger
}

func NewHandler(service *Service, log *logger.Logger) *Handler {
	return &Handler{service: service, log: log}
}

var _ authcontract.StrictServerInterface = (*Handler)(nil)

func (h *Handler) Login(
	ctx context.Context, request authcontract.LoginRequestObject,
) (authcontract.LoginResponseObject, error) {
	ctx = middleware.RequestContext(ctx)

	if request.Body == nil || request.Body.Email == "" || request.Body.Password == "" {
		return authcontract.Login400JSONResponse{Message: "email and password are required"}, nil
	}

	token, err := h.service.Login(ctx, request.Body.Email, request.Body.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		// Repeated failures for one email point at password guessing. The password itself is never logged.
		h.log.Warn("login failed", map[string]any{
			"request_id": middleware.RequestIDFrom(ctx),
			"email":      strings.ToLower(strings.TrimSpace(request.Body.Email)),
		})
		return authcontract.Login401JSONResponse{Message: err.Error()}, nil
	}
	if err != nil {
		return nil, err
	}

	return authcontract.Login200JSONResponse{
		AccessToken: token.AccessToken,
		TokenType:   tokenType,
		ExpiresIn:   int(token.ExpiresIn.Seconds()),
		Role:        authcontract.LoginResponseRole(token.Role),
	}, nil
}
