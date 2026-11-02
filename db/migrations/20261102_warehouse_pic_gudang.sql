-- Modul Stok Gudang: role PIC_GUDANG, soft-void movement, alur review→kirim→terima.

BEGIN;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
  ADD CONSTRAINT users_role_check
  CHECK (role IN ('admin', 'finance', 'cashier', 'manager', 'supervisor', 'pic_gudang'));

ALTER TABLE stock_movements
  ADD COLUMN IF NOT EXISTS voided_at TIMESTAMPTZ;
ALTER TABLE stock_movements
  ADD COLUMN IF NOT EXISTS voided_by TEXT;
ALTER TABLE stock_movements
  ADD COLUMN IF NOT EXISTS voided_by_name TEXT;
ALTER TABLE stock_movements
  ADD COLUMN IF NOT EXISTS void_reason TEXT;
ALTER TABLE stock_movements
  ADD COLUMN IF NOT EXISTS replaces_id TEXT;

CREATE INDEX IF NOT EXISTS stock_movements_voided_idx
  ON stock_movements (voided_at)
  WHERE voided_at IS NULL;

ALTER TABLE stock_request_items
  ADD COLUMN IF NOT EXISTS approved_quantity INTEGER;
ALTER TABLE stock_request_items
  ADD COLUMN IF NOT EXISTS shipped_quantity INTEGER;
ALTER TABLE stock_request_items
  ADD COLUMN IF NOT EXISTS received_quantity INTEGER;

ALTER TABLE stock_requests
  ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ;
ALTER TABLE stock_requests
  ADD COLUMN IF NOT EXISTS shipped_at TIMESTAMPTZ;
ALTER TABLE stock_requests
  ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ;
ALTER TABLE stock_requests
  ADD COLUMN IF NOT EXISTS shipped_by TEXT;
ALTER TABLE stock_requests
  ADD COLUMN IF NOT EXISTS shipped_by_name TEXT;
ALTER TABLE stock_requests
  ADD COLUMN IF NOT EXISTS received_by TEXT;
ALTER TABLE stock_requests
  ADD COLUMN IF NOT EXISTS received_by_name TEXT;

ALTER TABLE stock_requests DROP CONSTRAINT IF EXISTS stock_requests_status_check;
ALTER TABLE stock_requests
  ADD CONSTRAINT stock_requests_status_check CHECK (
    status IN (
      'pending',
      'approved',
      'rejected',
      'processing',
      'awaiting_shipment',
      'shipped',
      'received',
      'completed',
      'cancelled'
    )
  );

UPDATE stock_requests
SET status = 'awaiting_shipment', updated_at = now()
WHERE status = 'processing';

COMMIT;
