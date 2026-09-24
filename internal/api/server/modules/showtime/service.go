package showtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound         = errors.New("showtime not found")
	ErrHasBookings      = errors.New("showtime already has bookings")
	ErrStudioHasNoSeats = errors.New("studio has no seats")
)

// ValidationError is a caller mistake, answered with 400.
type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

// ConflictError clashes with data already stored, answered with 409.
type ConflictError struct{ Message string }

func (e ConflictError) Error() string { return e.Message }

type Input struct {
	MovieID  int64
	StudioID int64
	StartAt  time.Time
	Price    int64
}

type Service struct {
	showtimes *Repository
	now       func() time.Time
}

func NewService(showtimes *Repository) *Service {
	return &Service{showtimes: showtimes, now: time.Now}
}

// List expects Page and Limit set; the handler fills in defaults.
func (s *Service) List(ctx context.Context, f Filter) ([]Showtime, int64, error) {
	switch {
	case f.Page < 1 || f.Page > maxPage:
		return nil, 0, invalid("page must be between 1 and %d", maxPage)
	case f.Limit < 1 || f.Limit > maxLimit:
		return nil, 0, invalid("limit must be between 1 and %d", maxLimit)
	case f.From != nil && f.To != nil && !f.From.Before(*f.To):
		return nil, 0, invalid("from must be before to")
	}

	rows, total, err := s.showtimes.List(ctx, f)
	if err != nil {
		return nil, 0, fmt.Errorf("list showtimes: %w", err)
	}
	return rows, total, nil
}

func (s *Service) Get(ctx context.Context, id int64) (Showtime, error) {
	return s.showtimes.Get(ctx, id)
}

func (s *Service) Create(ctx context.Context, in Input) (Showtime, error) {
	row, err := s.build(ctx, in)
	if err != nil {
		return Showtime{}, err
	}
	row.Status = statusScheduled

	if err := s.showtimes.Create(ctx, &row); err != nil {
		return Showtime{}, saveError(err, row.StudioID)
	}
	return s.showtimes.Get(ctx, row.ID) // re-read for names
}

func (s *Service) Update(ctx context.Context, id int64, in Input) (Showtime, error) {
	row, err := s.build(ctx, in)
	if err != nil {
		return Showtime{}, err
	}
	row.ID = id

	if err := s.showtimes.Update(ctx, &row); err != nil {
		return Showtime{}, saveError(err, row.StudioID)
	}
	return s.showtimes.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return conflict(s.showtimes.Delete(ctx, id))
}

// build validates the input and derives end_at and studio_free_at.
func (s *Service) build(ctx context.Context, in Input) (Showtime, error) {
	switch {
	case in.MovieID < 1:
		return Showtime{}, invalid("movie_id is required")
	case in.StudioID < 1:
		return Showtime{}, invalid("studio_id is required")
	case in.Price < 1:
		// A missing price decodes as 0; reject it rather than sell free tickets.
		return Showtime{}, invalid("price must be greater than 0")
	case in.StartAt.IsZero():
		return Showtime{}, invalid("start_at is required")
	case !in.StartAt.After(s.now()):
		return Showtime{}, invalid("start_at must be in the future")
	}

	duration, err := s.showtimes.MovieDuration(ctx, in.MovieID)
	if errors.Is(err, ErrNotFound) {
		return Showtime{}, invalid("movie %d does not exist", in.MovieID)
	}
	if err != nil {
		return Showtime{}, fmt.Errorf("movie duration: %w", err)
	}

	exists, err := s.showtimes.StudioExists(ctx, in.StudioID)
	if err != nil {
		return Showtime{}, fmt.Errorf("find studio: %w", err)
	}
	if !exists {
		return Showtime{}, invalid("studio %d does not exist", in.StudioID)
	}

	end := in.StartAt.Add(duration)
	return Showtime{
		MovieID:      in.MovieID,
		StudioID:     in.StudioID,
		StartAt:      in.StartAt,
		EndAt:        end,
		StudioFreeAt: end.Add(cleaningTime),
		Price:        in.Price,
	}, nil
}

// saveError maps a seatless studio to 400; anything else goes through conflict.
func saveError(err error, studioID int64) error {
	if errors.Is(err, ErrStudioHasNoSeats) {
		return invalid("studio %d has no seats", studioID)
	}
	return conflict(err)
}

// conflict turns the database's constraint violations into a ConflictError.
func conflict(err error) error {
	if errors.Is(err, ErrHasBookings) {
		return ConflictError{Message: err.Error()}
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case exclusionViolation:
			return ConflictError{Message: fmt.Sprintf("overlaps another showtime in this studio, "+
				"including %d minutes of cleaning", int(cleaningTime.Minutes()))}
		case foreignKeyViolation:
			return ConflictError{Message: ErrHasBookings.Error()}
		}
	}
	return err
}

func invalid(format string, args ...any) error {
	return ValidationError{Message: fmt.Sprintf(format, args...)}
}
