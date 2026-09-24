-- Cinema ticketing schema (PostgreSQL 13+).
-- Create the database first:  psql -U postgres -c "CREATE DATABASE ticketing"
-- Then import:               psql -U postgres -d ticketing -f db/schema.sql

BEGIN;

-- Needed for "=" on studio_id in the GiST exclusion constraint.
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name          VARCHAR(100) NOT NULL,
    email         VARCHAR(255) NOT NULL UNIQUE CHECK (email = lower(email)),
    password_hash VARCHAR(255) NOT NULL,
    role          VARCHAR(20)  NOT NULL DEFAULT 'customer' CHECK (role IN ('admin', 'customer')),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);
COMMENT ON TABLE users IS 'Customers and cinema admins. Email is stored lower-cased.';
COMMENT ON COLUMN users.password_hash IS 'bcrypt hash, never the plain password';

CREATE TABLE cities (
    id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE
);
COMMENT ON TABLE cities IS 'Cities the cinema chain operates in';

CREATE TABLE cinemas (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    city_id    BIGINT       NOT NULL REFERENCES cities (id),
    name       VARCHAR(100) NOT NULL,
    address    TEXT         NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (city_id, name)
);
COMMENT ON TABLE cinemas IS 'A branch of the chain in a city';

CREATE TABLE studios (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cinema_id BIGINT      NOT NULL REFERENCES cinemas (id),
    name      VARCHAR(50) NOT NULL,
    UNIQUE (cinema_id, name)
);
COMMENT ON TABLE studios IS 'A screening room inside a cinema';

CREATE TABLE seats (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    studio_id   BIGINT     NOT NULL REFERENCES studios (id),
    row_label   VARCHAR(2) NOT NULL,
    seat_number SMALLINT   NOT NULL CHECK (seat_number > 0),
    UNIQUE (studio_id, row_label, seat_number)
);
COMMENT ON TABLE seats IS 'Physical seat layout of a studio, e.g. row A seat 5';

CREATE TABLE movies (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title            VARCHAR(200) NOT NULL,
    duration_minutes SMALLINT     NOT NULL CHECK (duration_minutes > 0),
    rating           VARCHAR(10)  NOT NULL,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);
COMMENT ON COLUMN movies.rating IS 'Age rating, e.g. SU, 13+, 17+, 21+';

CREATE TABLE showtimes (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    movie_id   BIGINT      NOT NULL REFERENCES movies (id),
    studio_id  BIGINT      NOT NULL REFERENCES studios (id),
    start_at   TIMESTAMPTZ NOT NULL,
    end_at     TIMESTAMPTZ NOT NULL,
    -- Stored, not computed: exclusion constraints need immutable expressions.
    studio_free_at TIMESTAMPTZ NOT NULL,
    price      BIGINT      NOT NULL CHECK (price > 0),
    status     VARCHAR(20) NOT NULL DEFAULT 'SCHEDULED' CHECK (status IN ('SCHEDULED', 'CANCELLED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_at > start_at),
    CHECK (studio_free_at >= end_at),
    -- No two scheduled showtimes overlap in a studio (cleaning included), even concurrently.
    CONSTRAINT showtimes_no_overlap EXCLUDE USING gist (
        studio_id WITH =,
        tstzrange(start_at, studio_free_at) WITH &&
    ) WHERE (status = 'SCHEDULED')
);
CREATE INDEX showtimes_movie_id_idx ON showtimes (movie_id);
CREATE INDEX showtimes_start_at_idx ON showtimes (start_at);
COMMENT ON TABLE showtimes IS 'Jadwal tayang: one movie in one studio at one time';
COMMENT ON COLUMN showtimes.end_at IS 'start_at + movie duration, set by the API';
COMMENT ON COLUMN showtimes.studio_free_at IS 'end_at + 15 minutes of cleaning; the next showtime may start from here';
COMMENT ON COLUMN showtimes.price IS 'Ticket price in Rupiah';

CREATE TABLE bookings (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    booking_code VARCHAR(20) NOT NULL UNIQUE,
    user_id      BIGINT      NOT NULL REFERENCES users (id),
    showtime_id  BIGINT      NOT NULL REFERENCES showtimes (id),
    status       VARCHAR(20) NOT NULL DEFAULT 'PENDING_PAYMENT'
                 CHECK (status IN ('PENDING_PAYMENT', 'PAID', 'EXPIRED', 'REFUNDED')),
    total_amount BIGINT      NOT NULL CHECK (total_amount >= 0),
    expires_at   TIMESTAMPTZ NOT NULL,
    paid_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((status = 'PAID') = (paid_at IS NOT NULL) OR status = 'REFUNDED'),
    -- Target of the composite FKs below: seats and tickets only point at their own showtime's bookings.
    UNIQUE (id, showtime_id)
);
CREATE INDEX bookings_user_id_idx ON bookings (user_id);
CREATE INDEX bookings_showtime_id_idx ON bookings (showtime_id);
COMMENT ON TABLE bookings IS 'A customer order. PENDING_PAYMENT holds its seats until expires_at';
COMMENT ON COLUMN bookings.expires_at IS 'Payment deadline; 1 minute after it (grace period) the held seats are free again';

CREATE TABLE showtime_seats (
    showtime_id BIGINT NOT NULL REFERENCES showtimes (id) ON DELETE CASCADE,
    seat_id     BIGINT NOT NULL REFERENCES seats (id),
    booking_id  BIGINT,
    PRIMARY KEY (showtime_id, seat_id),
    FOREIGN KEY (booking_id, showtime_id) REFERENCES bookings (id, showtime_id)
);
CREATE INDEX showtime_seats_booking_id_idx ON showtime_seats (booking_id);
COMMENT ON TABLE showtime_seats IS 'Seat stock per showtime, generated when the showtime is created';
COMMENT ON COLUMN showtime_seats.booking_id IS
    'Booking holding the seat. NULL, or a booking that is EXPIRED/REFUNDED/over 1 minute past expires_at, means available';

CREATE TABLE tickets (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    booking_id  BIGINT      NOT NULL,
    showtime_id BIGINT      NOT NULL,
    seat_id     BIGINT      NOT NULL,
    price       BIGINT      NOT NULL CHECK (price >= 0),
    status      VARCHAR(20) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'VOID')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (booking_id, showtime_id) REFERENCES bookings (id, showtime_id),
    FOREIGN KEY (showtime_id, seat_id) REFERENCES showtime_seats (showtime_id, seat_id)
);
-- One live ticket per seat per showtime.
CREATE UNIQUE INDEX tickets_one_active_per_seat ON tickets (showtime_id, seat_id) WHERE status = 'ACTIVE';
CREATE INDEX tickets_booking_id_idx ON tickets (booking_id);
COMMENT ON TABLE tickets IS 'Issued once a booking is PAID; VOID after a refund';

CREATE TABLE payments (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    booking_id   BIGINT      NOT NULL REFERENCES bookings (id),
    amount       BIGINT      NOT NULL CHECK (amount > 0),
    method       VARCHAR(30) NOT NULL,
    status       VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'SUCCESS', 'FAILED')),
    provider_ref VARCHAR(100) UNIQUE,
    paid_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX payments_booking_id_idx ON payments (booking_id);
COMMENT ON COLUMN payments.provider_ref IS 'Transaction id from the payment gateway; UNIQUE makes callbacks idempotent';

CREATE TABLE refunds (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payment_id   BIGINT      NOT NULL REFERENCES payments (id),
    amount       BIGINT      NOT NULL CHECK (amount > 0),
    reason       TEXT        NOT NULL,
    initiated_by VARCHAR(20) NOT NULL CHECK (initiated_by IN ('customer', 'cinema')),
    status       VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'SUCCESS', 'FAILED')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);
CREATE INDEX refunds_payment_id_idx ON refunds (payment_id);
COMMENT ON TABLE refunds IS 'Money returned to the customer, e.g. when the cinema cancels a showtime';

COMMIT;
