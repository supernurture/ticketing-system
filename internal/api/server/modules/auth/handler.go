package auth

import (
	"context"
	"errors"

	authcontract "ticketing-system/internal/api/server/oapicodegen/auth"
	"ticketing-system/internal/middleware"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
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
		return authcontract.Login401JSONResponse{Message: err.Error()}, nil
	}
	if err != nil {
		return nil, err
	}

	return authcontract.Login200JSONResponse{
		AccessToken: token.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(token.ExpiresIn.Seconds()),
		Role:        authcontract.LoginResponseRole(token.Role),
	}, nil
}
