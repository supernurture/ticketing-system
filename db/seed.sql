-- Sample data. Import after schema.sql:  psql -U postgres -d ticketing -f db/seed.sql
-- Logins:  admin@mkp.test / Admin123!   customer@mkp.test / Customer123!

BEGIN;

INSERT INTO users (name, email, password_hash, role) VALUES
    ('Cinema Admin', 'admin@mkp.test', '$2a$10$4omIHbj0/X4vBxdx8xGBqub.sUV2jRe8K0zB/H0CsGz5Z58IyTRoS', 'admin'),
    ('Budi Customer', 'customer@mkp.test', '$2a$10$CHItcZdGTaN.9ZYG1ip71uSOWV2Y37Q.UnoDB8ztGxT5fC17mKYCy', 'customer');

INSERT INTO cities (name) VALUES ('Jakarta'), ('Semarang'), ('Denpasar');

INSERT INTO cinemas (city_id, name, address)
SELECT id, name || ' Central', 'Jl. Contoh No. 1, ' || name FROM cities;

INSERT INTO studios (cinema_id, name)
SELECT c.id, s.name FROM cinemas c CROSS JOIN (VALUES ('Studio 1'), ('Studio 2')) AS s (name);

-- Every studio: rows A-E, seats 1-10.
INSERT INTO seats (studio_id, row_label, seat_number)
SELECT st.id, r.label, n
FROM studios st
CROSS JOIN (VALUES ('A'), ('B'), ('C'), ('D'), ('E')) AS r (label)
CROSS JOIN generate_series(1, 10) AS n;

INSERT INTO movies (title, duration_minutes, rating) VALUES
    ('Pengabdi Setan 3', 120, '17+'),
    ('Jumbo 2', 100, 'SU');

-- Tomorrow in WIB so showtimes stay in the future; 15 minutes cleaning, as in the API.
INSERT INTO showtimes (movie_id, studio_id, start_at, end_at, studio_free_at, price)
SELECT m.id, st.id, t.start_at, t.end_at, t.end_at + interval '15 minutes', v.price
FROM (VALUES
    ('Pengabdi Setan 3', 'Jakarta Central', 'Studio 1', time '13:00', 50000),
    ('Jumbo 2',          'Jakarta Central', 'Studio 1', time '16:00', 45000),
    ('Jumbo 2',          'Semarang Central', 'Studio 1', time '14:00', 40000)
) AS v (movie, cinema, studio, at, price)
JOIN movies m ON m.title = v.movie
JOIN cinemas c ON c.name = v.cinema
JOIN studios st ON st.cinema_id = c.id AND st.name = v.studio
CROSS JOIN LATERAL (
    SELECT ((current_date + 1) + v.at) AT TIME ZONE 'Asia/Jakarta' AS start_at
) AS s
CROSS JOIN LATERAL (
    SELECT s.start_at, s.start_at + make_interval(mins => m.duration_minutes) AS end_at
) AS t
ORDER BY c.name, v.at;

-- Seat stock per showtime, as the API creates it.
INSERT INTO showtime_seats (showtime_id, seat_id)
SELECT sh.id, se.id FROM showtimes sh JOIN seats se ON se.studio_id = sh.studio_id;

-- Paid booking (A1, A2 on showtime 1): has tickets and blocks deleting that showtime.
WITH sh AS (SELECT id, price FROM showtimes ORDER BY id LIMIT 1),
b AS (
    INSERT INTO bookings (booking_code, user_id, showtime_id, status, total_amount, expires_at, paid_at)
    SELECT 'BK-0001', u.id, sh.id, 'PAID', sh.price * 2, now() + interval '15 minutes', now()
    FROM sh, users u WHERE u.email = 'customer@mkp.test'
    RETURNING id, showtime_id, total_amount
),
held AS (
    UPDATE showtime_seats ss SET booking_id = b.id
    FROM b, seats se
    WHERE ss.showtime_id = b.showtime_id AND ss.seat_id = se.id
      AND se.row_label = 'A' AND se.seat_number IN (1, 2)
    RETURNING ss.showtime_id, ss.seat_id, ss.booking_id
),
pay AS (
    INSERT INTO payments (booking_id, amount, method, status, provider_ref, paid_at)
    SELECT id, total_amount, 'QRIS', 'SUCCESS', 'PG-0001', now() FROM b
)
INSERT INTO tickets (booking_id, showtime_id, seat_id, price)
SELECT held.booking_id, held.showtime_id, held.seat_id, sh.price FROM held, sh;

-- Pending booking (B5 on showtime 2), held for 15 minutes.
WITH sh AS (SELECT id, price FROM showtimes ORDER BY id OFFSET 1 LIMIT 1),
b AS (
    INSERT INTO bookings (booking_code, user_id, showtime_id, total_amount, expires_at)
    SELECT 'BK-0002', u.id, sh.id, sh.price, now() + interval '15 minutes'
    FROM sh, users u WHERE u.email = 'customer@mkp.test'
    RETURNING id, showtime_id
)
UPDATE showtime_seats ss SET booking_id = b.id
FROM b, seats se
WHERE ss.showtime_id = b.showtime_id AND ss.seat_id = se.id AND se.row_label = 'B' AND se.seat_number = 5;

COMMIT;
