# HALF POS API

API HTTP kasir HALF POS. Satu binary Go, query PostgreSQL lewat sqlc dan pgx.

## Menjalankan

Go 1.27.2 dipasang di `.tools/go` (tidak ikut commit). `make` memasangnya bila folder itu belum ada.

```bash
export DATABASE_URL='postgres://user:pass@localhost:5432/halfpos?sslmode=disable'
make run
```

`HTTP_ADDR` default-nya `:8080`. Variabel yang sama boleh ditaruh di file `.env` di root proyek; file itu tidak di-commit. Endpoint tidak memakai token.

Database PostgreSQL 14+ sudah memakai skema yang ada. Layanan ini tidak membuat tabel baru dan tidak mengubah arti kolom.

Pool memakai `DB_MIN_CONNS` (default 2), `DB_MAX_CONNS` (default 20), dan `DB_MAX_CONN_LIFETIME` (default `30m`).

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
