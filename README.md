# HALF POS API

API HTTP kasir HALF POS. Satu binary Go, query PostgreSQL lewat sqlc dan pgx.

## Menjalankan

```bash
export DATABASE_URL='postgres://user:pass@localhost:5432/halfpos?sslmode=disable'
export HTTP_ADDR=':8080'
export TOKEN_SECRET='ganti-dengan-rahasia-panjang'
go run ./cmd/api
```

Database PostgreSQL 14+ sudah memakai skema yang ada. Layanan ini tidak membuat tabel baru dan tidak mengubah arti kolom.

Pool memakai `DB_MIN_CONNS` (default 2), `DB_MAX_CONNS` (default 20), dan `DB_MAX_CONN_LIFETIME` (default `30m`). Token HMAC kedaluwarsa mengikuti `TOKEN_TTL` (default `24h`).

## sqlc

SQL ada di `db/query`. Hasil generate masuk ke `internal/store`.

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
sqlc generate
```

## Tes

```bash
go test ./...
```

Tes hash scrypt tidak butuh database. Tes transaksi memakai `HALF_TEST_DATABASE_URL` bila diisi. Bila kosong, tes mencoba Postgres di Docker, lalu PostgreSQL 16 lokal bila Docker tidak berjalan.
