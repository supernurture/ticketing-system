package showtime

import (
	"context"
	"errors"

	contract "ticketing-system/internal/api/server/oapicodegen/showtime"
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

var _ contract.StrictServerInterface = (*Handler)(nil)

var (
	forbidden  = contract.ForbiddenJSONResponse{Message: "admin only"}
	notFound   = contract.NotFoundJSONResponse{Message: "showtime not found"}
	bodyNeeded = contract.BadRequestJSONResponse{Message: "a JSON body is required"}
)

func (h *Handler) ListShowtimes(
	ctx context.Context, request contract.ListShowtimesRequestObject,
) (contract.ListShowtimesResponseObject, error) {
	params := request.Params
	f := Filter{
		MovieID: params.MovieId, CinemaID: params.CinemaId, From: params.From, To: params.To,
		Page: defaultPage, Limit: defaultLimit,
	}
	// Defaults only for parameters that were not sent, so an explicit page=0 or limit=0 is rejected.
	if params.Page != nil {
		f.Page = *params.Page
	}
	if params.Limit != nil {
		f.Limit = *params.Limit
	}

	rows, total, err := h.service.List(middleware.RequestContext(ctx), f)
	if msg, ok := asValidation(err); ok {
		return contract.ListShowtimes400JSONResponse{BadRequestJSONResponse: badRequest(msg)}, nil
	}
	if err != nil {
		return nil, err
	}

	data := make([]contract.Showtime, 0, len(rows))
	for _, row := range rows {
		data = append(data, toContract(row))
	}
	return contract.ListShowtimes200JSONResponse{Data: data, Page: f.Page, Limit: f.Limit, Total: total}, nil
}

func (h *Handler) GetShowtime(
	ctx context.Context, request contract.GetShowtimeRequestObject,
) (contract.GetShowtimeResponseObject, error) {
	row, err := h.service.Get(middleware.RequestContext(ctx), request.Id)
	if errors.Is(err, ErrNotFound) {
		return contract.GetShowtime404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if err != nil {
		return nil, err
	}
	return contract.GetShowtime200JSONResponse(toContract(row)), nil
}

func (h *Handler) CreateShowtime(
	ctx context.Context, request contract.CreateShowtimeRequestObject,
) (contract.CreateShowtimeResponseObject, error) {
	ctx = middleware.RequestContext(ctx)
	if !isAdmin(ctx) {
		return contract.CreateShowtime403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
	}
	if request.Body == nil {
		return contract.CreateShowtime400JSONResponse{BadRequestJSONResponse: bodyNeeded}, nil
	}

	row, err := h.service.Create(ctx, toInput(*request.Body))
	if msg, ok := asValidation(err); ok {
		return contract.CreateShowtime400JSONResponse{BadRequestJSONResponse: badRequest(msg)}, nil
	}
	if msg, ok := asConflict(err); ok {
		return contract.CreateShowtime409JSONResponse{ConflictJSONResponse: conflictResponse(msg)}, nil
	}
	if err != nil {
		return nil, err
	}
	h.audit(ctx, "showtime created", auditFields(row))
	return contract.CreateShowtime201JSONResponse(toContract(row)), nil
}

func (h *Handler) UpdateShowtime(
	ctx context.Context, request contract.UpdateShowtimeRequestObject,
) (contract.UpdateShowtimeResponseObject, error) {
	ctx = middleware.RequestContext(ctx)
	if !isAdmin(ctx) {
		return contract.UpdateShowtime403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
	}
	if request.Body == nil {
		return contract.UpdateShowtime400JSONResponse{BadRequestJSONResponse: bodyNeeded}, nil
	}

	row, err := h.service.Update(ctx, request.Id, toInput(*request.Body))
	if msg, ok := asValidation(err); ok {
		return contract.UpdateShowtime400JSONResponse{BadRequestJSONResponse: badRequest(msg)}, nil
	}
	if errors.Is(err, ErrNotFound) {
		return contract.UpdateShowtime404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if msg, ok := asConflict(err); ok {
		return contract.UpdateShowtime409JSONResponse{ConflictJSONResponse: conflictResponse(msg)}, nil
	}
	if err != nil {
		return nil, err
	}
	h.audit(ctx, "showtime updated", auditFields(row))
	return contract.UpdateShowtime200JSONResponse(toContract(row)), nil
}

func (h *Handler) DeleteShowtime(
	ctx context.Context, request contract.DeleteShowtimeRequestObject,
) (contract.DeleteShowtimeResponseObject, error) {
	ctx = middleware.RequestContext(ctx)
	if !isAdmin(ctx) {
		return contract.DeleteShowtime403JSONResponse{ForbiddenJSONResponse: forbidden}, nil
	}

	err := h.service.Delete(ctx, request.Id)
	if errors.Is(err, ErrNotFound) {
		return contract.DeleteShowtime404JSONResponse{NotFoundJSONResponse: notFound}, nil
	}
	if msg, ok := asConflict(err); ok {
		return contract.DeleteShowtime409JSONResponse{ConflictJSONResponse: conflictResponse(msg)}, nil
	}
	if err != nil {
		return nil, err
	}
	h.audit(ctx, "showtime deleted", map[string]any{"showtime_id": request.Id})
	return contract.DeleteShowtime204Response{}, nil
}

func isAdmin(ctx context.Context) bool {
	return middleware.ClaimsFrom(ctx).Role == middleware.RoleAdmin
}

// audit logs an admin change with the request ID and the admin's user ID, so any change to a showtime
// can be traced to who made it and matched with the access-log line of the same request.
func (h *Handler) audit(ctx context.Context, msg string, fields map[string]any) {
	fields["request_id"] = middleware.RequestIDFrom(ctx)
	fields["user_id"] = middleware.ClaimsFrom(ctx).Subject
	h.log.Info(msg, fields)
}

// auditFields records the values a showtime was saved with.
func auditFields(row Showtime) map[string]any {
	return map[string]any{
		"showtime_id": row.ID,
		"movie_id":    row.MovieID,
		"studio_id":   row.StudioID,
		"start_at":    row.StartAt,
		"price":       row.Price,
	}
}

func asValidation(err error) (string, bool) {
	var invalid ValidationError
	if errors.As(err, &invalid) {
		return invalid.Message, true
	}
	return "", false
}

func asConflict(err error) (string, bool) {
	var clash ConflictError
	if errors.As(err, &clash) {
		return clash.Message, true
	}
	return "", false
}

func badRequest(msg string) contract.BadRequestJSONResponse {
	return contract.BadRequestJSONResponse{Message: msg}
}

func conflictResponse(msg string) contract.ConflictJSONResponse {
	return contract.ConflictJSONResponse{Message: msg}
}

func toInput(body contract.ShowtimeRequest) Input {
	return Input{MovieID: body.MovieId, StudioID: body.StudioId, StartAt: body.StartAt, Price: body.Price}
}

func toContract(row Showtime) contract.Showtime {
	return contract.Showtime{
		Id:         row.ID,
		MovieId:    row.MovieID,
		MovieTitle: row.MovieTitle,
		CinemaId:   row.CinemaID,
		CinemaName: row.CinemaName,
		StudioId:   row.StudioID,
		StudioName: row.StudioName,
		StartAt:    row.StartAt,
		EndAt:      row.EndAt,
		Price:      row.Price,
		Status:     contract.ShowtimeStatus(row.Status),
	}
}
