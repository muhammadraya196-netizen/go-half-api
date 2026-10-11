-- name: ListStockMovements :many
SELECT
  id, transaction_number, movement_type, product_id, product_name, category_name,
  supplier_name, unit_code, quantity, stock_before, stock_after, purchase_price,
  total_price, reason, note, officer_id, officer_name, created_at
FROM stock_movements
WHERE
  (sqlc.narg('product_id')::text IS NULL OR product_id = sqlc.narg('product_id'))
  AND (sqlc.narg('movement_type')::text IS NULL OR movement_type = sqlc.narg('movement_type'))
  AND (
    sqlc.narg('supplier')::text IS NULL
    OR supplier_name = sqlc.narg('supplier')
  )
  AND (sqlc.narg('start_at')::timestamptz IS NULL OR created_at >= sqlc.narg('start_at'))
  AND (sqlc.narg('end_before')::timestamptz IS NULL OR created_at < sqlc.narg('end_before'))
  AND (
    sqlc.narg('search')::text IS NULL
    OR product_name ILIKE sqlc.narg('search')
    OR transaction_number ILIKE sqlc.narg('search')
    OR COALESCE(supplier_name, '') ILIKE sqlc.narg('search')
  )
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::text)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.narg('row_limit')::integer
OFFSET sqlc.arg('row_offset')::integer;

-- name: CountStockMovements :one
SELECT COUNT(*)::bigint AS total
FROM stock_movements
WHERE
  (sqlc.narg('product_id')::text IS NULL OR product_id = sqlc.narg('product_id'))
  AND (sqlc.narg('movement_type')::text IS NULL OR movement_type = sqlc.narg('movement_type'))
  AND (
    sqlc.narg('supplier')::text IS NULL
    OR supplier_name = sqlc.narg('supplier')
  )
  AND (sqlc.narg('start_at')::timestamptz IS NULL OR created_at >= sqlc.narg('start_at'))
  AND (sqlc.narg('end_before')::timestamptz IS NULL OR created_at < sqlc.narg('end_before'))
  AND (
    sqlc.narg('search')::text IS NULL
    OR product_name ILIKE sqlc.narg('search')
    OR transaction_number ILIKE sqlc.narg('search')
    OR COALESCE(supplier_name, '') ILIKE sqlc.narg('search')
  );

-- name: LockProduct :one
SELECT p.id, p.name, p.stock, c.name AS category_name
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE p.id = sqlc.arg('id')
FOR UPDATE OF p;

-- name: GetSupplierByName :one
SELECT id, name
FROM suppliers
WHERE name = sqlc.arg('name');

-- name: GetUnit :one
SELECT code
FROM units
WHERE code = sqlc.arg('code');

-- name: UpdateProductStock :exec
UPDATE products
SET stock = sqlc.arg('stock'), updated_at = now()
WHERE id = sqlc.arg('id');

-- name: InsertStockMovement :one
INSERT INTO stock_movements (
  id, transaction_number, movement_type, product_id, product_name, category_name,
  supplier_id, supplier_name, unit_code, quantity, stock_before, stock_after,
  purchase_price, reason, note, officer_id, officer_name
) VALUES (
  sqlc.arg('id'),
  sqlc.arg('transaction_number'),
  sqlc.arg('movement_type'),
  sqlc.arg('product_id'),
  sqlc.arg('product_name'),
  sqlc.arg('category_name'),
  sqlc.narg('supplier_id'),
  sqlc.narg('supplier_name'),
  sqlc.narg('unit_code'),
  sqlc.arg('quantity'),
  sqlc.arg('stock_before'),
  sqlc.arg('stock_after'),
  sqlc.narg('purchase_price'),
  sqlc.narg('reason'),
  sqlc.narg('note'),
  sqlc.arg('officer_id'),
  sqlc.arg('officer_name')
)
RETURNING created_at, total_price;

-- name: MovementAggregates :many
WITH bounds AS (
  SELECT
    (date_trunc('day', now() AT TIME ZONE 'Asia/Jakarta') AT TIME ZONE 'Asia/Jakarta') AS start_at,
    ((date_trunc('day', now() AT TIME ZONE 'Asia/Jakarta') + interval '1 day') AT TIME ZONE 'Asia/Jakarta') AS end_at
)
SELECT
  m.product_id,
  COALESCE(SUM(CASE WHEN m.movement_type = 'in' THEN m.quantity ELSE 0 END), 0)::bigint AS total_in,
  COALESCE(SUM(CASE WHEN m.movement_type = 'out' THEN m.quantity ELSE 0 END), 0)::bigint AS total_out,
  COALESCE(SUM(
    CASE
      WHEN m.movement_type = 'in' AND m.created_at >= b.start_at AND m.created_at < b.end_at THEN m.quantity
      ELSE 0
    END
  ), 0)::bigint AS today_in,
  COALESCE(SUM(
    CASE
      WHEN m.movement_type = 'out' AND m.created_at >= b.start_at AND m.created_at < b.end_at THEN m.quantity
      ELSE 0
    END
  ), 0)::bigint AS today_out
FROM stock_movements m
CROSS JOIN bounds b
GROUP BY m.product_id;
