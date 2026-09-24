package showtime

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ticketing-system/pkg/database"
)

type Showtime struct {
	ID           int64 `gorm:"primaryKey"`
	MovieID      int64
	StudioID     int64
	StartAt      time.Time
	EndAt        time.Time
	StudioFreeAt time.Time
	Price        int64
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time

	// Read-only ("->"), filled by withNames on reads and never written back.
	MovieTitle string `gorm:"->"`
	CinemaID   int64  `gorm:"->"`
	CinemaName string `gorm:"->"`
	StudioName string `gorm:"->"`
}

// joined brings in the movie, studio and cinema of each showtime, for filtering and for withNames.
func joined(db *gorm.DB) *gorm.DB {
	return db.Table("showtimes").
		Joins("JOIN movies ON movies.id = showtimes.movie_id").
		Joins("JOIN studios ON studios.id = showtimes.studio_id").
		Joins("JOIN cinemas ON cinemas.id = studios.cinema_id")
}

// withNames selects what a customer needs to recognise a showtime: movie title, cinema and studio names.
// Kept apart from joined because Count cannot take this multi-column select.
func withNames(db *gorm.DB) *gorm.DB {
	return joined(db).Select("showtimes.*, movies.title AS movie_title, cinemas.id AS cinema_id, " +
		"cinemas.name AS cinema_name, studios.name AS studio_name")
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
	filtered := func(db *gorm.DB) *gorm.DB {
		if f.MovieID != nil {
			db = db.Where("showtimes.movie_id = ?", *f.MovieID)
		}
		if f.CinemaID != nil {
			db = db.Where("cinemas.id = ?", *f.CinemaID)
		}
		if f.From != nil {
			db = db.Where("showtimes.start_at >= ?", *f.From)
		}
		if f.To != nil {
			db = db.Where("showtimes.start_at < ?", *f.To)
		}
		return db
	}

	var total int64
	if err := r.db.WithContext(ctx).Scopes(joined, filtered).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []Showtime
	err := r.db.WithContext(ctx).Scopes(withNames, filtered).
		Order("showtimes.start_at, showtimes.id").
		Offset((f.Page - 1) * f.Limit).Limit(f.Limit).
		Find(&rows).Error
	return rows, total, err
}

func (r *Repository) Get(ctx context.Context, id int64) (Showtime, error) {
	var row Showtime
	err := r.db.WithContext(ctx).Scopes(withNames).Where("showtimes.id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Showtime{}, ErrNotFound
	}
	return row, err
}

// MovieDuration returns the movie's running time, or ErrNotFound.
func (r *Repository) MovieDuration(ctx context.Context, movieID int64) (time.Duration, error) {
	var minutes []int
	err := r.db.WithContext(ctx).Table("movies").Where("id = ?", movieID).Pluck("duration_minutes", &minutes).Error
	if err != nil {
		return 0, err
	}
	if len(minutes) == 0 {
		return 0, ErrNotFound
	}
	return time.Duration(minutes[0]) * time.Minute, nil
}

func (r *Repository) StudioExists(ctx context.Context, studioID int64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Table("studios").Where("id = ?", studioID).Count(&count).Error
	return count > 0, err
}

// Create stores the showtime and copies its studio's seats into showtime_seats, the stock customers book from.
func (r *Repository) Create(ctx context.Context, row *Showtime) error {
	return database.WithTransaction(ctx, r.db, func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
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
		var current Showtime
		// Row lock: a booking cannot slip in between the check below and the update.
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current, row.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		var bookings int64
		err = tx.Table("bookings").
			Where("showtime_id = ?", row.ID).
			Where("status = 'PAID' OR (status = 'PENDING_PAYMENT' AND expires_at > now())").
			Count(&bookings).Error
		if err != nil {
			return err
		}
		if bookings > 0 {
			return ErrHasBookings
		}

		row.Status, row.CreatedAt = current.Status, current.CreatedAt
		if err := tx.Save(row).Error; err != nil {
			return err
		}
		if row.StudioID == current.StudioID {
			return nil
		}
		if err := tx.Exec("DELETE FROM showtime_seats WHERE showtime_id = ?", row.ID).Error; err != nil {
			return err
		}
		return generateSeats(tx, row)
	})
}

// Delete returns ErrNotFound, or the foreign-key violation from bookings when the showtime has any.
func (r *Repository) Delete(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Delete(&Showtime{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func generateSeats(tx *gorm.DB, row *Showtime) error {
	return tx.Exec(`INSERT INTO showtime_seats (showtime_id, seat_id) SELECT ?, id FROM seats WHERE studio_id = ?`,
		row.ID, row.StudioID).Error
}
