package showtime

import "time"

// cleaningTime is the gap after each showtime, enforced via studio_free_at.
const cleaningTime = 15 * time.Minute

const (
	defaultPage  = 1
	defaultLimit = 20
	maxLimit     = 100
	// maxPage keeps (page-1)*limit far from int overflow.
	maxPage = 100000

	statusScheduled = "SCHEDULED"

	// fromShowtimesJoined joins each showtime's movie, studio and cinema.
	fromShowtimesJoined = `
		FROM showtimes s
		JOIN movies m   ON m.id = s.movie_id
		JOIN studios st ON st.id = s.studio_id
		JOIN cinemas c  ON c.id = st.cinema_id`

	// selectWithNames reads a showtime with its movie title, cinema and studio names.
	selectWithNames = `
		SELECT s.id, s.movie_id, s.studio_id, s.start_at, s.end_at, s.studio_free_at, s.price, s.status,
		       s.created_at, s.updated_at,
		       m.title AS movie_title, c.id AS cinema_id, c.name AS cinema_name, st.name AS studio_name` +
		fromShowtimesJoined

	// SQLSTATEs Postgres raises for the constraints in db/schema.sql.
	exclusionViolation  = "23P01" // showtimes_no_overlap
	foreignKeyViolation = "23503" // bookings.showtime_id on delete; tickets on a studio change
)
