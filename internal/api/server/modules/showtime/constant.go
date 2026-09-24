package showtime

import "time"

// cleaningTime keeps the studio free after each showtime; the overlap constraint covers it via studio_free_at.
const cleaningTime = 15 * time.Minute

const (
	defaultPage  = 1
	defaultLimit = 20
	maxLimit     = 100
	// maxPage keeps (page-1)*limit far from int overflow.
	maxPage = 100000

	statusScheduled = "SCHEDULED"

	// SQLSTATEs Postgres raises for the constraints in db/schema.sql.
	exclusionViolation  = "23P01" // showtimes_no_overlap
	foreignKeyViolation = "23503" // bookings.showtime_id on delete; tickets on a studio change
)
