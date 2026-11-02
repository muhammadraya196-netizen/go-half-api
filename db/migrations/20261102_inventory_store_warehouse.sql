-- Dual inventory: products.stock = Stok Toko (POS), warehouse_stock = Stok Gudang.
-- Request workflow + audit columns. Riwayat tidak dihapus.

BEGIN;

ALTER TABLE products
  ADD COLUMN IF NOT EXISTS warehouse_stock INTEGER NOT NULL DEFAULT 0;

ALTER TABLE products
  DROP CONSTRAINT IF EXISTS products_warehouse_stock_check;

ALTER TABLE products
  ADD CONSTRAINT products_warehouse_stock_check CHECK (warehouse_stock >= 0);

COMMENT ON COLUMN products.stock IS
  'Stok Toko — saldo siap jual di POS.';
COMMENT ON COLUMN products.warehouse_stock IS
  'Stok Gudang — persediaan di gudang, belum siap jual di POS.';

-- Soft-hide dari daftar Stok Toko tanpa menghapus produk/riwayat.
ALTER TABLE products
  ADD COLUMN IF NOT EXISTS store_listed BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
  ADD CONSTRAINT users_role_check
  CHECK (role IN ('admin', 'finance', 'cashier', 'manager', 'supervisor'));

ALTER TABLE stock_movements
  ADD COLUMN IF NOT EXISTS location_from TEXT;

ALTER TABLE stock_movements
  ADD COLUMN IF NOT EXISTS location_to TEXT;

ALTER TABLE stock_movements
  ADD COLUMN IF NOT EXISTS movement_kind TEXT;

ALTER TABLE stock_movements
  ADD COLUMN IF NOT EXISTS reference_id TEXT;

ALTER TABLE stock_movements
  DROP CONSTRAINT IF EXISTS stock_movements_reason_check;

ALTER TABLE stock_movements
  ADD CONSTRAINT stock_movements_reason_check CHECK (
    reason IS NULL
    OR reason IN (
      'Penjualan',
      'Produk Rusak',
      'Kadaluarsa',
      'Sampel',
      'Retur',
      'Lainnya',
      'Produk Kedaluwarsa',
      'Kemasan Rusak',
      'Produk Tidak Layak Jual',
      'Kerusakan Lainnya',
      'Transfer Gudang ke Toko',
      'Penyesuaian Stok Toko',
      'Penyesuaian Stok Gudang'
    )
  );

ALTER TABLE stock_movements
  DROP CONSTRAINT IF EXISTS stock_movements_supplier_required_check;

ALTER TABLE stock_movements
  ADD CONSTRAINT stock_movements_supplier_required_check CHECK (
    movement_type = 'out'
    OR supplier_id IS NOT NULL
    OR COALESCE(movement_kind, '') IN (
      'TRANSFER_IN',
      'ADJUSTMENT_IN',
      'STORE_ADJUST_IN'
    )
  );

CREATE TABLE IF NOT EXISTS stock_requests (
  id TEXT PRIMARY KEY,
  request_number VARCHAR(40) NOT NULL UNIQUE,
  status TEXT NOT NULL,
  requested_by TEXT NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
  requested_by_name VARCHAR(100) NOT NULL,
  reviewed_by TEXT REFERENCES users (id) ON DELETE RESTRICT,
  reviewed_by_name VARCHAR(100),
  note TEXT,
  review_note TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT stock_requests_status_check CHECK (
    status IN (
      'pending',
      'approved',
      'rejected',
      'processing',
      'completed',
      'cancelled'
    )
  ),
  CONSTRAINT stock_requests_number_check CHECK (
    request_number ~ '^REQ-[0-9]{8}-[0-9]{3,}$'
  )
);

CREATE TABLE IF NOT EXISTS stock_request_items (
  id TEXT PRIMARY KEY,
  request_id TEXT NOT NULL REFERENCES stock_requests (id) ON DELETE CASCADE,
  product_id TEXT NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
  product_name VARCHAR(100) NOT NULL,
  sku VARCHAR(20) NOT NULL,
  quantity INTEGER NOT NULL,
  unit_code VARCHAR(20) NOT NULL DEFAULT 'pcs' REFERENCES units (code),
  CONSTRAINT stock_request_items_quantity_check CHECK (quantity > 0)
);

CREATE INDEX IF NOT EXISTS stock_requests_status_created_idx
  ON stock_requests (status, created_at DESC);

CREATE INDEX IF NOT EXISTS stock_request_items_request_idx
  ON stock_request_items (request_id);

CREATE INDEX IF NOT EXISTS stock_movements_kind_created_idx
  ON stock_movements (movement_kind, created_at DESC);

CREATE INDEX IF NOT EXISTS stock_movements_reference_idx
  ON stock_movements (reference_id);

-- Seed gudang awal agar request bisa diuji tanpa mengosongkan stok toko.
UPDATE products
SET warehouse_stock = GREATEST(warehouse_stock, GREATEST(stock * 3, 30))
WHERE warehouse_stock = 0;

COMMIT;
