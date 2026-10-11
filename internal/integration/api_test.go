package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	httpapi "halfpos/internal/http"
	"halfpos/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	scryptHash = "scrypt$0123456789abcdef0123456789abcdef$7bfc2ad8e90d7252b24c322d25a3ef314bb6f3b0ad29738a3140d921aa2606024e5f3f7eef2b45dfb16dd5428705bb539af7384b2504308175cc714be9c428bf"
	tokenKey   = "test-token-secret"
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dsn, cleanup, err := ensureDatabase(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pool, err := store.Open(ctx, dsn, 1, 8, time.Hour)
	if err != nil {
		cleanup()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := applySchema(ctx, pool.Pool()); err != nil {
		pool.Close()
		cleanup()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	shared = pool
	code := m.Run()
	pool.Close()
	cleanup()
	os.Exit(code)
}

var shared *store.Store

func ensureDatabase(ctx context.Context) (string, func(), error) {
	if dsn := os.Getenv("HALF_TEST_DATABASE_URL"); dsn != "" {
		return dsn, func() {}, nil
	}
	if dsn, cleanup, err := startDockerPostgres(); err == nil {
		return waitPostgres(ctx, dsn, cleanup)
	}
	return startLocalPostgres(ctx)
}

func startDockerPostgres() (string, func(), error) {
	name := "halfpos-api-test"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	out, err := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"-e", "POSTGRES_PASSWORD=postgres",
		"-e", "POSTGRES_DB=halfpos",
		"-p", "127.0.0.1:55432:5432",
		"postgres:16-alpine",
	).CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("docker postgres: %w: %s", err, out)
	}
	dsn := "postgres://postgres:postgres@127.0.0.1:55432/halfpos?sslmode=disable"
	return dsn, func() { _ = exec.Command("docker", "rm", "-f", name).Run() }, nil
}

func startLocalPostgres(ctx context.Context) (string, func(), error) {
	bin := "/opt/homebrew/opt/postgresql@16/bin"
	if _, err := os.Stat(filepath.Join(bin, "initdb")); err != nil {
		return "", nil, fmt.Errorf("postgres lokal dan docker tidak tersedia")
	}
	dir, err := os.MkdirTemp("", "halfpos-pg")
	if err != nil {
		return "", nil, err
	}
	data := filepath.Join(dir, "data")
	if out, err := exec.Command(filepath.Join(bin, "initdb"), "-D", data, "--username", "halfpos", "--auth", "trust").CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("initdb: %w: %s", err, out)
	}
	if out, err := exec.Command(filepath.Join(bin, "pg_ctl"), "-D", data, "-l", filepath.Join(dir, "log"), "-o", "-p 55432 -h 127.0.0.1 -k "+dir, "start").CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("pg_ctl: %w: %s", err, out)
	}
	cleanup := func() {
		_ = exec.Command(filepath.Join(bin, "pg_ctl"), "-D", data, "-m", "fast", "stop").Run()
		os.RemoveAll(dir)
	}
	dsn := "postgres://halfpos@127.0.0.1:55432/postgres?sslmode=disable"
	if _, _, err := waitPostgres(ctx, dsn, cleanup); err != nil {
		return "", nil, err
	}
	if out, err := exec.Command(filepath.Join(bin, "createdb"), "-h", "127.0.0.1", "-p", "55432", "-U", "halfpos", "halfpos").CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("createdb: %w: %s", err, out)
	}
	return "postgres://halfpos@127.0.0.1:55432/halfpos?sslmode=disable", cleanup, nil
}

func waitPostgres(ctx context.Context, dsn string, cleanup func()) (string, func(), error) {
	var last error
	for i := 0; i < 40; i++ {
		pool, err := pgxpool.New(ctx, dsn)
		if err == nil {
			err = pool.Ping(ctx)
			pool.Close()
			if err == nil {
				return dsn, cleanup, nil
			}
			last = err
		} else {
			last = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	cleanup()
	return "", nil, fmt.Errorf("postgres belum siap: %w", last)
}

func applySchema(ctx context.Context, pool *pgxpool.Pool) error {
	var exists *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.products')::text`).Scan(&exists); err != nil {
		return err
	}
	if exists != nil {
		return nil
	}
	body, err := os.ReadFile(schemaFile())
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	_, err = conn.Conn().PgConn().Exec(ctx, string(body)).ReadAll()
	return err
}

func schemaFile() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "db", "schema.sql")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "db/schema.sql"
		}
		dir = parent
	}
}

func reset(t *testing.T) {
	t.Helper()
	_, err := shared.Pool().Exec(context.Background(), `
		TRUNCATE TABLE
		  order_item_components, order_payments, order_items, stock_movements, orders,
		  promo_bundle_labels, promo_bundle_components, promo_bundles,
		  user_activities, products, users, document_sequences
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
}

func server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer((&httpapi.Server{
		Store: shared, Secret: []byte(tokenKey), TokenTTL: time.Hour, Timeout: 10 * time.Second,
	}).Router())
}

func doJSON(t *testing.T, ts *httptest.Server, method, path, token string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var payload map[string]any
	_ = json.NewDecoder(res.Body).Decode(&payload)
	return res, payload
}

func insertUser(t *testing.T, id, email, role, status, hash string) {
	t.Helper()
	_, err := shared.Pool().Exec(context.Background(), `
		INSERT INTO users (id, name, username, email, role, status, password_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, "Kasir Uji", strings.Split(email, "@")[0], email, role, status, hash)
	if err != nil {
		t.Fatal(err)
	}
}

func insertProduct(t *testing.T, id, sku, name, category string, price int64, stock int) {
	t.Helper()
	_, err := shared.Pool().Exec(context.Background(), `
		INSERT INTO products (id, sku, name, price, stock, category_id)
		VALUES ($1, $2, $3, $4, $5, $6)`, id, sku, name, price, stock, category)
	if err != nil {
		t.Fatal(err)
	}
}

func stockOf(t *testing.T, id string) int {
	t.Helper()
	var stock int
	if err := shared.Pool().QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, id).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	return stock
}

func countOrders(t *testing.T) int {
	t.Helper()
	var n int
	if err := shared.Pool().QueryRow(context.Background(), `SELECT COUNT(*) FROM orders`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLoginExistingScryptHash(t *testing.T) {
	reset(t)
	insertUser(t, "user-admin", "admin@half.test", "admin", "active", scryptHash)
	ts := server(t)
	defer ts.Close()

	res, body := doJSON(t, ts, http.MethodPost, "/api/auth/login", "", map[string]string{
		"email": "admin@half.test", "password": "password123",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status %d body %#v", res.StatusCode, body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "password") || strings.Contains(string(raw), scryptHash) {
		t.Fatal("respons login memuat password atau hash")
	}
	user := body["user"].(map[string]any)
	if user["id"] != "user-admin" || user["role"] != "admin" || user["email"] != "admin@half.test" {
		t.Fatalf("user %#v", user)
	}
	token, _ := body["token"].(string)
	if token == "" || !strings.Contains(token, ".") {
		t.Fatalf("token tidak ditandatangani: %q", token)
	}
	res, body = doJSON(t, ts, http.MethodPost, "/api/products", token, map[string]any{
		"name": "Kue Coklat", "sku": "SKU-NEW", "price": 15000, "stock": 3,
		"category": "Cake", "description": "",
	})
	if res.StatusCode != http.StatusCreated || body["category"] != "Cake" || body["price"] != float64(15000) {
		t.Fatalf("buat produk %d %#v", res.StatusCode, body)
	}

	var logged *time.Time
	if err := shared.Pool().QueryRow(context.Background(), `SELECT last_login FROM users WHERE id = 'user-admin'`).Scan(&logged); err != nil || logged == nil {
		t.Fatal("last_login tidak terisi", err)
	}

	res, body = doJSON(t, ts, http.MethodPost, "/api/auth/login", "", map[string]string{
		"email": "admin@half.test", "password": "salah-sekali",
	})
	if res.StatusCode != http.StatusUnauthorized || body["message"] != "Email atau password salah" {
		t.Fatalf("password salah: %d %#v", res.StatusCode, body)
	}

	if _, err := shared.Pool().Exec(context.Background(), `UPDATE users SET status = 'inactive' WHERE id = 'user-admin'`); err != nil {
		t.Fatal(err)
	}
	res, body = doJSON(t, ts, http.MethodPost, "/api/auth/login", "", map[string]string{
		"email": "admin@half.test", "password": "password123",
	})
	if res.StatusCode != http.StatusUnauthorized || body["message"] != "Akun tidak aktif" {
		t.Fatalf("akun inactive: %d %#v", res.StatusCode, body)
	}

	res, _ = doJSON(t, ts, http.MethodGet, "/api/products", "", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tanpa token: %d", res.StatusCode)
	}
}

func TestInsufficientStockWritesNothing(t *testing.T) {
	reset(t)
	insertUser(t, "user-kasir", "kasir@half.test", "cashier", "active", scryptHash)
	insertProduct(t, "prod-a", "SKU-A", "Roti A", "bread", 10000, 1)
	ts := server(t)
	defer ts.Close()
	token := login(t, ts, "kasir@half.test")
	res, body := doJSON(t, ts, http.MethodPost, "/api/orders", token, orderBody("user-kasir", "prod-a", "Roti A", 5, 10000))
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(fmt.Sprint(body["message"]), "Stok tidak cukup") {
		t.Fatalf("status %d %#v", res.StatusCode, body)
	}
	if countOrders(t) != 0 {
		t.Fatal("order tertulis padahal stok kurang")
	}
	if stockOf(t, "prod-a") != 1 {
		t.Fatal("stok berubah")
	}
	var movements int
	if err := shared.Pool().QueryRow(context.Background(), `SELECT COUNT(*) FROM stock_movements`).Scan(&movements); err != nil || movements != 0 {
		t.Fatal("jurnal tertulis", movements, err)
	}
}

func TestCheckoutMultiItemOneTransaction(t *testing.T) {
	reset(t)
	insertUser(t, "user-kasir", "kasir@half.test", "cashier", "active", scryptHash)
	insertProduct(t, "prod-a", "SKU-A", "Roti A", "bread", 8000, 10)
	insertProduct(t, "prod-b", "SKU-B", "Roti B", "bread", 10000, 8)
	if _, err := shared.Pool().Exec(context.Background(), `
		INSERT INTO promo_bundles (id, sku, name, description, price, original_price, promo_label)
		VALUES ('bundle-1', 'BND-1', 'Paket Roti', 'dua roti A', 20000, 30000, 'Hemat');
		INSERT INTO promo_bundle_components (bundle_id, product_id, quantity, sort_order)
		VALUES ('bundle-1', 'prod-a', 2, 0)`); err != nil {
		t.Fatal(err)
	}
	ts := server(t)
	defer ts.Close()
	token := login(t, ts, "kasir@half.test")
	res, body := doJSON(t, ts, http.MethodPost, "/api/orders", token, map[string]any{
		"items": []map[string]any{
			{"productId": "prod-b", "productName": "Roti B", "quantity": 1, "price": 10000},
			{"productId": "bundle-1", "productName": "Paket Roti", "quantity": 2, "price": 20000},
		},
		"subtotal": 50000, "tax": 0, "discount": 0, "total": 50000,
		"paymentMethod": "cash", "cashReceived": 60000, "changeAmount": 10000,
		"cashierId": "user-kasir", "cashierName": "Kasir Uji",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("checkout %d %#v", res.StatusCode, body)
	}
	if stockOf(t, "prod-a") != 6 || stockOf(t, "prod-b") != 7 {
		t.Fatalf("stok A=%d B=%d", stockOf(t, "prod-a"), stockOf(t, "prod-b"))
	}
	if countOrders(t) != 1 {
		t.Fatal("jumlah order")
	}
	var items, components, payments, movements int
	var number, reason string
	if err := shared.Pool().QueryRow(context.Background(), `
		SELECT
		  (SELECT COUNT(*) FROM order_items),
		  (SELECT COUNT(*) FROM order_item_components),
		  (SELECT COUNT(*) FROM order_payments),
		  (SELECT COUNT(*) FROM stock_movements),
		  (SELECT order_number FROM orders),
		  (SELECT reason FROM stock_movements ORDER BY product_id LIMIT 1)
	`).Scan(&items, &components, &payments, &movements, &number, &reason); err != nil {
		t.Fatal(err)
	}
	if items != 2 || components != 1 || payments != 1 || movements != 2 {
		t.Fatalf("items=%d components=%d payments=%d movements=%d", items, components, payments, movements)
	}
	if !strings.HasPrefix(number, "INV-") || reason != "Penjualan" {
		t.Fatalf("nomor %s alasan %s", number, reason)
	}
	res, body = doJSON(t, ts, http.MethodGet, "/api/orders?limit=10", token, nil)
	if res.StatusCode != http.StatusOK || body["total"] != float64(1) {
		t.Fatalf("daftar order %d %#v", res.StatusCode, body)
	}
	res, _ = doJSON(t, ts, http.MethodGet, "/api/orders", token, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("daftar order tanpa limit %d", res.StatusCode)
	}
	res, _ = doJSON(t, ts, http.MethodGet, "/api/products", token, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("daftar produk %d", res.StatusCode)
	}
	res, _ = doJSON(t, ts, http.MethodGet, "/api/stock/overview", token, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("ringkasan stok %d", res.StatusCode)
	}
	var distinctNumbers int
	if err := shared.Pool().QueryRow(context.Background(), `SELECT COUNT(DISTINCT transaction_number) FROM stock_movements`).Scan(&distinctNumbers); err != nil || distinctNumbers != 2 {
		t.Fatal("nomor STO tidak unik", distinctNumbers, err)
	}
}

func TestRollbackRestoresStock(t *testing.T) {
	reset(t)
	insertProduct(t, "prod-a", "SKU-A", "Roti A", "bread", 10000, 9)
	ctx := context.Background()
	tx, err := shared.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE products SET stock = stock - 4 WHERE id = 'prod-a'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if stockOf(t, "prod-a") != 9 {
		t.Fatalf("stok setelah rollback %d", stockOf(t, "prod-a"))
	}
}

func TestStockJournalRejectsUpdate(t *testing.T) {
	reset(t)
	insertUser(t, "user-kasir", "kasir@half.test", "cashier", "active", scryptHash)
	insertProduct(t, "prod-a", "SKU-A", "Roti A", "bread", 10000, 5)
	ts := server(t)
	defer ts.Close()
	token := login(t, ts, "kasir@half.test")
	res, body := doJSON(t, ts, http.MethodPost, "/api/stock", token, map[string]any{
		"type": "in", "productId": "prod-a", "supplier": "Coklat Nusantara", "unit": "pcs",
		"quantity": 2, "purchasePrice": 4000, "officerId": "user-kasir", "officerName": "Kasir Uji",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("stok masuk %d %#v", res.StatusCode, body)
	}
	id := fmt.Sprint(body["id"])
	_, err := shared.Pool().Exec(context.Background(), `UPDATE stock_movements SET note = 'ubah' WHERE id = $1`, id)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update jurnal harus ditolak: %v", err)
	}
	_, err = shared.Pool().Exec(context.Background(), `DELETE FROM stock_movements WHERE id = $1`, id)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete jurnal harus ditolak: %v", err)
	}
	if stockOf(t, "prod-a") != 7 {
		t.Fatalf("stok %d", stockOf(t, "prod-a"))
	}
}

func TestDeleteReferencedProductConflict(t *testing.T) {
	reset(t)
	insertUser(t, "user-admin", "admin@half.test", "admin", "active", scryptHash)
	insertProduct(t, "prod-a", "SKU-A", "Roti A", "bread", 10000, 4)
	ts := server(t)
	defer ts.Close()
	token := login(t, ts, "admin@half.test")
	res, body := doJSON(t, ts, http.MethodPost, "/api/orders", token, orderBody("user-admin", "prod-a", "Roti A", 1, 10000))
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("order %d %#v", res.StatusCode, body)
	}
	res, body = doJSON(t, ts, http.MethodDelete, "/api/products/prod-a", token, nil)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("hapus %d %#v", res.StatusCode, body)
	}
	if body["message"] != "Produk tidak dapat dihapus karena masih dipakai transaksi, stok, atau promo" {
		t.Fatalf("pesan %#v", body["message"])
	}
}

func TestDashboardUsesJakartaDay(t *testing.T) {
	reset(t)
	insertUser(t, "user-kasir", "kasir@half.test", "cashier", "active", scryptHash)
	insertProduct(t, "prod-a", "SKU-A", "Roti A", "bread", 10000, 20)
	ctx := context.Background()
	var start time.Time
	if err := shared.Pool().QueryRow(ctx, `
		SELECT (date_trunc('day', now() AT TIME ZONE 'Asia/Jakarta') AT TIME ZONE 'Asia/Jakarta')
	`).Scan(&start); err != nil {
		t.Fatal(err)
	}
	insertAt := func(id, number string, at time.Time, status string, qty int, total int64) {
		t.Helper()
		if _, err := shared.Pool().Exec(ctx, `
			INSERT INTO orders (
			  id, order_number, subtotal, tax, discount, total, payment_method, status,
			  cashier_id, cashier_name, change_amount, created_at
			) VALUES ($1, $2, $3, 0, 0, $3, 'cash', $4, 'user-kasir', 'Kasir Uji', 0, $5)`,
			id, number, total, status, at); err != nil {
			t.Fatal(err)
		}
		if _, err := shared.Pool().Exec(ctx, `
			INSERT INTO order_items (
			  id, order_id, product_id, product_name, quantity, unit_price, subtotal, sort_order
			) VALUES ($1, $2, 'prod-a', 'Roti A', $3, $4, $5, 0)`,
			id+"-1", id, qty, total/int64(qty), total); err != nil {
			t.Fatal(err)
		}
	}
	insertAt("ord-today", "INV-20200101-001", start, "completed", 4, 40000)
	insertAt("ord-yesterday", "INV-20200101-002", start.Add(-time.Second), "completed", 9, 90000)
	insertAt("ord-pending", "INV-20200101-003", start.Add(time.Minute), "pending", 3, 30000)

	ts := server(t)
	defer ts.Close()
	token := login(t, ts, "kasir@half.test")
	res, body := doJSON(t, ts, http.MethodGet, "/api/analytics", token, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("analytics %d %#v", res.StatusCode, body)
	}
	if body["todaySales"] != float64(40000) || body["todayOrderCount"] != float64(1) || body["todayItemsSold"] != float64(4) {
		t.Fatalf("metrik hari ini %#v", body)
	}
	if body["totalRevenue"] != float64(130000) || body["todayAverageSale"] != float64(40000) {
		t.Fatalf("omzet %#v", body)
	}
}

func TestConcurrentCheckoutDoesNotOversell(t *testing.T) {
	reset(t)
	insertUser(t, "user-kasir", "kasir@half.test", "cashier", "active", scryptHash)
	insertProduct(t, "prod-a", "SKU-A", "Roti A", "bread", 10000, 1)
	ts := server(t)
	defer ts.Close()
	token := login(t, ts, "kasir@half.test")
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			res, _ := doJSON(t, ts, http.MethodPost, "/api/orders", token, orderBody("user-kasir", "prod-a", "Roti A", 1, 10000))
			codes[i] = res.StatusCode
		}(i)
	}
	wg.Wait()
	ok, denied := 0, 0
	for _, code := range codes {
		if code == http.StatusCreated {
			ok++
		} else if code == http.StatusBadRequest {
			denied++
		}
	}
	if ok != 1 || denied != 1 || stockOf(t, "prod-a") != 0 || countOrders(t) != 1 {
		t.Fatalf("kode %v stok %d order %d", codes, stockOf(t, "prod-a"), countOrders(t))
	}
}

func login(t *testing.T, ts *httptest.Server, email string) string {
	t.Helper()
	res, body := doJSON(t, ts, http.MethodPost, "/api/auth/login", "", map[string]string{
		"email": email, "password": "password123",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login %s: %d %#v", email, res.StatusCode, body)
	}
	token, _ := body["token"].(string)
	return token
}

func orderBody(cashier, productID, name string, qty int, price int64) map[string]any {
	total := price * int64(qty)
	return map[string]any{
		"items": []map[string]any{{
			"productId": productID, "productName": name, "quantity": qty, "price": price,
		}},
		"subtotal": total, "tax": int64(0), "discount": int64(0), "total": total,
		"paymentMethod": "cash", "cashReceived": total, "changeAmount": int64(0),
		"cashierId": cashier, "cashierName": "Kasir Uji",
	}
}
