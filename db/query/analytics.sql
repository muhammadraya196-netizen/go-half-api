-- name: DashboardStats :one
WITH bounds AS (
  SELECT
    (date_trunc('day', now() AT TIME ZONE 'Asia/Jakarta') AT TIME ZONE 'Asia/Jakarta') AS start_at,
    ((date_trunc('day', now() AT TIME ZONE 'Asia/Jakarta') + interval '1 day') AT TIME ZONE 'Asia/Jakarta') AS end_at
)
SELECT
  (SELECT COUNT(*)::bigint FROM products) AS total_products,
  (SELECT COUNT(*)::bigint FROM orders) AS total_orders,
  (SELECT COUNT(*)::bigint FROM orders WHERE status = 'completed') AS completed_orders,
  (SELECT COALESCE(SUM(total), 0)::bigint FROM orders WHERE status = 'completed') AS total_revenue,
  (
    SELECT COALESCE(SUM(o.total), 0)::bigint
    FROM orders o, bounds b
    WHERE o.status = 'completed'
      AND o.created_at >= b.start_at
      AND o.created_at < b.end_at
  ) AS today_sales,
  (
    SELECT COUNT(*)::bigint
    FROM orders o, bounds b
    WHERE o.status = 'completed'
      AND o.created_at >= b.start_at
      AND o.created_at < b.end_at
  ) AS today_order_count,
  (
    SELECT COALESCE(SUM(i.quantity), 0)::bigint
    FROM order_items i
    JOIN orders o ON o.id = i.order_id
    CROSS JOIN bounds b
    WHERE o.status = 'completed'
      AND o.created_at >= b.start_at
      AND o.created_at < b.end_at
  ) AS today_items_sold
FROM bounds;

-- name: UserExists :one
SELECT id
FROM users
WHERE id = sqlc.arg('id');
