-- name: ListOrders :many
SELECT
  o.id, o.order_number, o.subtotal, o.tax, o.discount, o.total,
  o.payment_method, o.status, o.cashier_id, o.cashier_name, o.customer_name,
  o.notes, o.cash_received, o.change_amount, o.created_at,
  COALESCE((
    SELECT jsonb_agg(
      jsonb_build_object(
        'productId', COALESCE(i.bundle_id, i.product_id),
        'productName', i.product_name,
        'quantity', i.quantity,
        'price', i.unit_price::bigint,
        'subtotal', i.subtotal::bigint,
        'bundleComponents', COALESCE((
          SELECT jsonb_agg(
            jsonb_build_object(
              'productId', c.product_id,
              'productName', c.product_name,
              'quantity', c.quantity
            )
            ORDER BY c.product_id
          )
          FROM order_item_components c
          WHERE c.order_item_id = i.id
        ), '[]'::jsonb)
      )
      ORDER BY i.sort_order
    )
    FROM order_items i
    WHERE i.order_id = o.id
  ), '[]'::jsonb)::text AS items,
  COALESCE((
    SELECT jsonb_agg(
      jsonb_build_object(
        'method', p.method,
        'amount', p.amount::bigint,
        'cashReceived', p.cash_received::bigint
      )
      ORDER BY p.sort_order
    )
    FROM order_payments p
    WHERE p.order_id = o.id
  ), '[]'::jsonb)::text AS payments
FROM orders o
WHERE
  (sqlc.narg('status')::text IS NULL OR o.status = sqlc.narg('status'))
  AND (sqlc.narg('start_at')::timestamptz IS NULL OR o.created_at >= sqlc.narg('start_at'))
  AND (sqlc.narg('end_before')::timestamptz IS NULL OR o.created_at < sqlc.narg('end_before'))
  AND (
    sqlc.narg('search')::text IS NULL
    OR o.order_number ILIKE sqlc.narg('search')
    OR o.cashier_name ILIKE sqlc.narg('search')
    OR COALESCE(o.customer_name, '') ILIKE sqlc.narg('search')
  )
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR (
      sqlc.arg('sort_by') = 'date'
      AND sqlc.arg('sort_dir') = 'asc'
      AND (o.created_at, o.id) > (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::text)
    )
    OR (
      sqlc.narg('cursor_created_at')::timestamptz IS NOT NULL
      AND NOT (sqlc.arg('sort_by') = 'date' AND sqlc.arg('sort_dir') = 'asc')
      AND (o.created_at, o.id) < (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::text)
    )
  )
ORDER BY
  CASE WHEN sqlc.arg('sort_by') = 'total' AND sqlc.arg('sort_dir') = 'asc' THEN o.total END ASC,
  CASE WHEN sqlc.arg('sort_by') = 'total' AND sqlc.arg('sort_dir') <> 'asc' THEN o.total END DESC,
  CASE WHEN sqlc.arg('sort_by') <> 'total' AND sqlc.arg('sort_dir') = 'asc' THEN o.created_at END ASC,
  CASE WHEN sqlc.arg('sort_by') <> 'total' AND sqlc.arg('sort_dir') <> 'asc' THEN o.created_at END DESC,
  o.id DESC
LIMIT sqlc.narg('row_limit')::integer
OFFSET sqlc.arg('row_offset')::integer;

-- name: CountOrders :one
SELECT COUNT(*)::bigint AS total
FROM orders o
WHERE
  (sqlc.narg('status')::text IS NULL OR o.status = sqlc.narg('status'))
  AND (sqlc.narg('start_at')::timestamptz IS NULL OR o.created_at >= sqlc.narg('start_at'))
  AND (sqlc.narg('end_before')::timestamptz IS NULL OR o.created_at < sqlc.narg('end_before'))
  AND (
    sqlc.narg('search')::text IS NULL
    OR o.order_number ILIKE sqlc.narg('search')
    OR o.cashier_name ILIKE sqlc.narg('search')
    OR COALESCE(o.customer_name, '') ILIKE sqlc.narg('search')
  );

-- name: LockProductsForCart :many
SELECT p.id, p.name, p.stock, c.name AS category_name
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE p.id = ANY(sqlc.arg('item_ids')::text[])
   OR p.id IN (
     SELECT bc.product_id
     FROM promo_bundle_components bc
     WHERE bc.bundle_id = ANY(sqlc.arg('item_ids')::text[])
   )
ORDER BY p.id
FOR UPDATE OF p;

-- name: ListPromoBundles :many
SELECT id, name
FROM promo_bundles
WHERE id = ANY(sqlc.arg('ids')::text[]);

-- name: ListBundleRecipes :many
SELECT bc.bundle_id, bc.product_id, bc.quantity, bc.sort_order
FROM promo_bundle_components bc
WHERE bc.bundle_id = ANY(sqlc.arg('bundle_ids')::text[])
ORDER BY bc.bundle_id, bc.sort_order;

-- name: NextDocumentNumbers :one
SELECT next_document_numbers(sqlc.arg('prefix'), sqlc.arg('count')::integer) AS last_value;

-- name: InsertOrder :one
INSERT INTO orders (
  id, order_number, subtotal, tax, discount, total, payment_method, status,
  cashier_id, cashier_name, customer_name, notes, cash_received, change_amount
) VALUES (
  sqlc.arg('id'),
  sqlc.arg('order_number'),
  sqlc.arg('subtotal'),
  sqlc.arg('tax'),
  sqlc.arg('discount'),
  sqlc.arg('total'),
  sqlc.arg('payment_method'),
  sqlc.arg('status'),
  sqlc.arg('cashier_id'),
  sqlc.arg('cashier_name'),
  sqlc.narg('customer_name'),
  sqlc.narg('notes'),
  sqlc.narg('cash_received'),
  sqlc.arg('change_amount')
)
RETURNING created_at;

-- name: InsertOrderItems :exec
INSERT INTO order_items (
  id, order_id, product_id, bundle_id, product_name, quantity, unit_price, subtotal, sort_order
)
SELECT
  r.id, sqlc.arg('order_id'), r.product_id, r.bundle_id, r.product_name, r.quantity,
  r.unit_price, r.subtotal, r.sort_order
FROM jsonb_to_recordset(sqlc.arg('rows')::jsonb) AS r(
  id text,
  product_id text,
  bundle_id text,
  product_name text,
  quantity integer,
  unit_price numeric,
  subtotal numeric,
  sort_order integer
);

-- name: InsertOrderItemComponents :exec
INSERT INTO order_item_components (order_item_id, product_id, product_name, quantity)
SELECT r.order_item_id, r.product_id, r.product_name, r.quantity
FROM jsonb_to_recordset(sqlc.arg('rows')::jsonb) AS r(
  order_item_id text,
  product_id text,
  product_name text,
  quantity integer
);

-- name: InsertOrderPayments :exec
INSERT INTO order_payments (id, order_id, sort_order, method, amount, cash_received)
SELECT r.id, sqlc.arg('order_id'), r.sort_order, r.method, r.amount, r.cash_received
FROM jsonb_to_recordset(sqlc.arg('rows')::jsonb) AS r(
  id text,
  sort_order integer,
  method text,
  amount numeric,
  cash_received numeric
);

-- name: UpdateProductStocks :exec
UPDATE products AS p
SET stock = v.stock, updated_at = now()
FROM jsonb_to_recordset(sqlc.arg('rows')::jsonb) AS v(id text, stock integer)
WHERE p.id = v.id;

-- name: InsertSaleStockMovements :exec
INSERT INTO stock_movements (
  id, transaction_number, movement_type, product_id, product_name, category_name,
  quantity, stock_before, stock_after, reason, note, officer_id, officer_name, order_id
)
SELECT
  r.id, r.transaction_number, 'out', r.product_id, r.product_name, r.category_name,
  r.quantity, r.stock_before, r.stock_after, 'Penjualan', r.note,
  r.officer_id, r.officer_name, r.order_id
FROM jsonb_to_recordset(sqlc.arg('rows')::jsonb) AS r(
  id text,
  transaction_number text,
  product_id text,
  product_name text,
  category_name text,
  quantity integer,
  stock_before integer,
  stock_after integer,
  note text,
  officer_id text,
  officer_name text,
  order_id text
);

-- name: CountOrdersByID :one
SELECT COUNT(*)::bigint AS total
FROM orders
WHERE id = sqlc.arg('id');
