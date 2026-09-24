package showtime

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"ticketing-system/pkg/database"
)

type Showtime struct {
	ID           int64
	MovieID      int64
	StudioID     int64
	StartAt      time.Time
	EndAt        time.Time
	StudioFreeAt time.Time
	Price        int64
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time

	// Filled only by reads (selectWithNames), never written.
	MovieTitle string
	CinemaID   int64
	CinemaName string
	StudioName string
}

type Filter struct {
	MovieID  *int64
	CinemaID *int64
	From     *time.Time
	To       *time.Time
	Page     int
	Limit    int
}

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, f Filter) ([]Showtime, int64, error) {
	// Every value goes through a placeholder; only these fixed condition strings are joined into the SQL.
	var conds []string
	var args []any
	where := func(cond string, arg any) {
		conds = append(conds, cond)
		args = append(args, arg)
	}
	if f.MovieID != nil {
		where("s.movie_id = ?", *f.MovieID)
	}
	if f.CinemaID != nil {
		where("c.id = ?", *f.CinemaID)
	}
	if f.From != nil {
		where("s.start_at >= ?", *f.From)
	}
	if f.To != nil {
		where("s.start_at < ?", *f.To)
	}
	whereSQL := ""
	if len(conds) > 0 {
		whereSQL = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int64
	err := r.db.WithContext(ctx).Raw("SELECT count(*)"+fromShowtimesJoined+whereSQL, args...).Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	var rows []Showtime
	err = r.db.WithContext(ctx).Raw(selectWithNames+whereSQL+" ORDER BY s.start_at, s.id LIMIT ? OFFSET ?",
		append(args, f.Limit, (f.Page-1)*f.Limit)...).Scan(&rows).Error
	return rows, total, err
}

func (r *Repository) Get(ctx context.Context, id int64) (Showtime, error) {
	var row Showtime
	result := r.db.WithContext(ctx).Raw(selectWithNames+" WHERE s.id = ?", id).Scan(&row)
	if result.Error != nil {
		return Showtime{}, result.Error
	}
	if result.RowsAffected == 0 {
		return Showtime{}, ErrNotFound
	}
	return row, nil
}

// MovieDuration returns the movie's running time, or ErrNotFound.
func (r *Repository) MovieDuration(ctx context.Context, movieID int64) (time.Duration, error) {
	var minutes int
	result := r.db.WithContext(ctx).Raw(`SELECT duration_minutes FROM movies WHERE id = ?`, movieID).Scan(&minutes)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, ErrNotFound
	}
	return time.Duration(minutes) * time.Minute, nil
}

func (r *Repository) StudioExists(ctx context.Context, studioID int64) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM studios WHERE id = ?)`, studioID).Scan(&exists).Error
	return exists, err
}

// Create stores the showtime and copies its studio's seats into showtime_seats, the stock customers book from.
// It sets row.ID; the service re-reads the row for everything else.
func (r *Repository) Create(ctx context.Context, row *Showtime) error {
	return database.WithTransaction(ctx, r.db, func(tx *gorm.DB) error {
		err := tx.Raw(`
			INSERT INTO showtimes (movie_id, studio_id, start_at, end_at, studio_free_at, price, status)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			RETURNING id`,
			row.MovieID, row.StudioID, row.StartAt, row.EndAt, row.StudioFreeAt, row.Price, row.Status,
		).Scan(&row.ID).Error
		if err != nil {
			return err
		}
		return generateSeats(tx, row)
	})
}

// Update replaces the showtime's fields. It returns ErrNotFound, or ErrHasBookings while a booking is live
// (paid, or waiting for payment within its deadline), so a ticket a customer holds never moves under them.
// An expired booking does not block the update.
func (r *Repository) Update(ctx context.Context, row *Showtime) error {
	return database.WithTransaction(ctx, r.db, func(tx *gorm.DB) error {
		// Row lock: a booking cannot slip in between the check below and the update.
		var currentStudioID int64
		result := tx.Raw(`SELECT studio_id FROM showtimes WHERE id = ? FOR UPDATE`, row.ID).Scan(&currentStudioID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}

		var liveBookings int64
		err := tx.Raw(`
			SELECT count(*) FROM bookings
			WHERE showtime_id = ?
			  AND (status = 'PAID' OR (status = 'PENDING_PAYMENT' AND expires_at > now()))`,
			row.ID).Scan(&liveBookings).Error
		if err != nil {
			return err
		}
		if liveBookings > 0 {
			return ErrHasBookings
		}

		err = tx.Exec(`
			UPDATE showtimes
			SET movie_id = ?, studio_id = ?, start_at = ?, end_at = ?, studio_free_at = ?, price = ?,
			    updated_at = now()
			WHERE id = ?`,
			row.MovieID, row.StudioID, row.StartAt, row.EndAt, row.StudioFreeAt, row.Price, row.ID,
		).Error
		if err != nil {
			return err
		}
		if row.StudioID == currentStudioID {
			return nil
		}
		if err := tx.Exec(`DELETE FROM showtime_seats WHERE showtime_id = ?`, row.ID).Error; err != nil {
			return err
		}
		return generateSeats(tx, row)
	})
}

// Delete returns ErrNotFound, or the foreign-key violation from bookings when the showtime has any.
func (r *Repository) Delete(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM showtimes WHERE id = ?`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// generateSeats returns ErrStudioHasNoSeats when the studio has no seat layout yet: a showtime without
// seat stock could never sell a ticket, so the caller's transaction is rolled back instead.
func generateSeats(tx *gorm.DB, row *Showtime) error {
	result := tx.Exec(`INSERT INTO showtime_seats (showtime_id, seat_id) SELECT ?, id FROM seats WHERE studio_id = ?`,
		row.ID, row.StudioID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrStudioHasNoSeats
	}
	return nil
}
