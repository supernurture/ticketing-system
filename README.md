# Ticketing System — Backend Test MKP 2025

**Nama lengkap:** Yonathan Dawang Jati

Platform pembelian tiket bioskop online: customer bisa membeli tiket kapan pun tanpa khawatir kursinya dipakai orang lain.

| Soal | Jawaban |
|---|---|
| A. System Design | Topologi [`docs/topology.jpg`](docs/topology.jpg), flowchart untuk orang awam [`docs/flowchart.jpg`](docs/flowchart.jpg) (sumber `.drawio` di folder yang sama), penjelasan di [`docs/system-design.md`](docs/system-design.md) |
| B. Database Design | ERD [`docs/erd.jpg`](docs/erd.jpg), detail per tabel [`docs/db/`](docs/db/README.md), script [`db/schema.sql`](db/schema.sql) + data contoh [`db/seed.sql`](db/seed.sql) |
| C. Skill Test (Go) | API Login + CRUD jadwal tayang, Postman: [`docs/postman/ticketing-system.postman_collection.json`](docs/postman/ticketing-system.postman_collection.json) |

## Menjalankan

Butuh:
- **Go 1.21+.** Proyek ini memakai Go 1.26.8; Go 1.21 ke atas otomatis mengunduh versi itu saat pertama kali `go run`/`go test`.
- **PostgreSQL 13+ (lokal)** dengan extension `btree_gist`. Extension ini sudah termasuk di installer Windows/macOS. Di Linux, pasang paket `postgresql-contrib` bila `CREATE EXTENSION btree_gist` gagal.

```bash
# 1. Database
psql -U postgres -c "CREATE DATABASE ticketing"
psql -U postgres -d ticketing -f db/schema.sql
psql -U postgres -d ticketing -f db/seed.sql

# 2. Konfigurasi (sesuaikan user/password Postgres di configs/config.yaml atau .env bila perlu)
cp configs/config.example.yaml configs/config.yaml
cp .env.example .env

# 3. Jalankan API di http://localhost:8080
go run ./cmd/api

# 4. Cek server sudah jalan (terminal lain)
curl http://localhost:8080/health
# {"condition":"Healthy"}
```

Lalu import Postman collection dan jalankan dari atas ke bawah.

### Reset database

`schema.sql` hanya bisa dijalankan pada database kosong. Untuk mengulang dari awal (mis. setelah salah langkah atau setelah mencoba Postman berkali-kali):

```bash
psql -U postgres -c "DROP DATABASE IF EXISTS ticketing"
psql -U postgres -c "CREATE DATABASE ticketing"
psql -U postgres -d ticketing -f db/schema.sql
psql -U postgres -d ticketing -f db/seed.sql
```

Hentikan API dulu sebelum `DROP DATABASE`, karena Postgres menolak menghapus database yang masih punya koneksi.

Akun dari `seed.sql`:

| Role | Email | Password |
|---|---|---|
| admin | `admin@mkp.test` | `Admin123!` |
| customer | `customer@mkp.test` | `Customer123!` |

## API

Semua route jadwal tayang butuh header `Authorization: Bearer <token>` dari login.

| Method | Endpoint | Untuk | Keterangan |
|---|---|---|---|
| POST | `/api/v1/auth/login` | semua user | email + password → JWT (HS256, 1 jam) |
| GET | `/api/v1/showtimes` | user login | filter `movie_id`, `cinema_id`, `from`, `to`; paginasi `page` (1–100000), `limit` (1–100) |
| GET | `/api/v1/showtimes/{id}` | user login | detail jadwal; semua respons jadwal menyertakan judul film, nama bioskop, dan studio |
| POST | `/api/v1/showtimes` | admin | `end_at` dihitung dari durasi film; kursi studio jadi stok kursi jadwal |
| PUT | `/api/v1/showtimes/{id}` | admin | 409 jika masih ada booking lunas atau menunggu bayar (booking kedaluwarsa tidak menghalangi) |
| DELETE | `/api/v1/showtimes/{id}` | admin | 409 jika jadwal pernah punya booking (riwayat disimpan) |

Kode status: `400` input salah, `401` token tidak ada/invalid/kedaluwarsa, `403` bukan admin, `404` tidak ditemukan, `409` jadwal bentrok dengan jadwal lain di studio yang sama (termasuk 15 menit waktu bersih-bersih studio), atau jadwal sudah punya booking.

Filter `from`/`to` memakai format RFC 3339. Tanda `+` pada zona waktu harus ditulis `%2B` di URL, misalnya `from=2026-01-01T00:00:00%2B07:00`, karena `+` di URL dibaca sebagai spasi.

Kontrak lengkap ada di [`api/server/specs/`](api/server/specs) (OpenAPI). Kode server di-generate dengan `make oapicodegen`.

## Race condition

Pengamanannya ada di database, jadi tetap berlaku walaupun API dijalankan di banyak server sekaligus:

- **Kursi**: dikunci dengan satu `UPDATE` bersyarat saat booking (`PENDING_PAYMENT`, batas bayar 15 menit, lalu 1 menit masa tenggang sebelum kursi boleh diambil orang lain). Hanya satu transaksi yang bisa memegang satu kursi, dan `UNIQUE` di `tickets` mencegah tiket ganda. Detail: [`docs/system-design.md`](docs/system-design.md).
- **Jadwal bentrok**: constraint `EXCLUDE USING gist (studio_id WITH =, tstzrange(start_at, studio_free_at) WITH &&)`, dengan `studio_free_at` = selesai film + 15 menit bersih-bersih. Dua admin yang membuat jadwal bentrok di detik yang sama tetap hanya satu yang berhasil (409 untuk yang lain).
- **Update jadwal**: baris jadwal dikunci (`SELECT ... FOR UPDATE`) sebelum dicek masih punya booking aktif atau tidak, jadi booking baru tidak bisa menyelip di antara pengecekan dan update.

## Struktur

```
api/server/specs/     OpenAPI spec per modul (auth, showtime, health)
cmd/api/              entry point
configs/              contoh konfigurasi
db/                   schema.sql, seed.sql
docs/                 diagram, ERD, penjelasan system design, Postman collection (docs/postman)
internal/api/server/  router, kode hasil generate, modul auth & showtime (handler → service → repository)
internal/middleware/  request ID, log, recovery, timeout, CORS, auth JWT
pkg/                  database (GORM + Postgres), logger
```

Dibangun dari template [supernurture/go-template](https://github.com/supernurture/go-template) (layout mengikuti [golang-standards/project-layout](https://github.com/golang-standards/project-layout)); bagian yang tidak dipakai (Redis, SQL Server, HTTP client, Docker, modul contoh) sudah dihapus.

## Pengembangan

```bash
go test ./...          # unit test, tidak butuh database (DB di-mock dengan sqlmock)
make oapicodegen       # generate ulang kode dari spec (butuh make + bash)
```

`make test` menjalankan test yang sama dengan `-race`, yang di Windows butuh GCC (CGO). Tanpa GCC, pakai `go test ./...`.
