-- name: ListProducts :many
SELECT
  p.id, p.sku, p.name, p.description, p.price, p.stock,
  c.name AS category_name, p.image, p.rating,
  p.is_best_seller, p.is_customer_favorite, p.badge, p.min_stock,
  p.created_at, p.updated_at
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE
  (
    sqlc.narg('search')::text IS NULL
    OR p.name ILIKE sqlc.narg('search')
    OR p.sku ILIKE sqlc.narg('search')
    OR c.name ILIKE sqlc.narg('search')
  )
  AND (
    sqlc.narg('category')::text IS NULL
    OR c.name = sqlc.narg('category')
    OR c.id = sqlc.narg('category')
  )
  AND (
    sqlc.narg('status')::text IS NULL
    OR (sqlc.narg('status') = 'active' AND p.stock > 0)
    OR (sqlc.narg('status') = 'inactive' AND p.stock = 0)
  )
  AND (sqlc.narg('min_price')::numeric IS NULL OR p.price >= sqlc.narg('min_price'))
  AND (sqlc.narg('max_price')::numeric IS NULL OR p.price <= sqlc.narg('max_price'))
  AND (sqlc.narg('min_stock')::integer IS NULL OR p.stock >= sqlc.narg('min_stock'))
ORDER BY p.created_at ASC,
  CASE WHEN p.id ~ '^[0-9]+$' THEN lpad(p.id, 12, '0') ELSE p.id END
LIMIT sqlc.narg('row_limit')::integer
OFFSET sqlc.arg('row_offset')::integer;

-- name: CountProducts :one
SELECT COUNT(*)::bigint AS total
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE
  (
    sqlc.narg('search')::text IS NULL
    OR p.name ILIKE sqlc.narg('search')
    OR p.sku ILIKE sqlc.narg('search')
    OR c.name ILIKE sqlc.narg('search')
  )
  AND (
    sqlc.narg('category')::text IS NULL
    OR c.name = sqlc.narg('category')
    OR c.id = sqlc.narg('category')
  )
  AND (
    sqlc.narg('status')::text IS NULL
    OR (sqlc.narg('status') = 'active' AND p.stock > 0)
    OR (sqlc.narg('status') = 'inactive' AND p.stock = 0)
  )
  AND (sqlc.narg('min_price')::numeric IS NULL OR p.price >= sqlc.narg('min_price'))
  AND (sqlc.narg('max_price')::numeric IS NULL OR p.price <= sqlc.narg('max_price'))
  AND (sqlc.narg('min_stock')::integer IS NULL OR p.stock >= sqlc.narg('min_stock'));

-- name: GetProduct :one
SELECT
  p.id, p.sku, p.name, p.description, p.price, p.stock,
  c.name AS category_name, p.image, p.rating,
  p.is_best_seller, p.is_customer_favorite, p.badge, p.min_stock,
  p.created_at, p.updated_at
FROM products p
JOIN categories c ON c.id = p.category_id
WHERE p.id = sqlc.arg('id');

-- name: CreateProduct :one
WITH inserted AS (
  INSERT INTO products (id, sku, name, description, price, stock, category_id)
  SELECT
    sqlc.arg('id'),
    sqlc.arg('sku'),
    sqlc.arg('name'),
    sqlc.narg('description'),
    sqlc.arg('price'),
    sqlc.arg('stock'),
    picked.id
  FROM (
    SELECT categories.id
    FROM categories
    WHERE categories.name = sqlc.arg('category') OR categories.id = sqlc.arg('category_key')
    LIMIT 1
  ) AS picked
  RETURNING
    id, sku, name, description, price, stock, category_id, image, rating,
    is_best_seller, is_customer_favorite, badge, min_stock, created_at, updated_at
)
SELECT
  i.id, i.sku, i.name, i.description, i.price, i.stock,
  c.name AS category_name, i.image,
  COALESCE(i.rating::text, '') AS rating,
  i.is_best_seller, i.is_customer_favorite, i.badge, i.min_stock,
  i.created_at, i.updated_at
FROM inserted i
JOIN categories c ON c.id = i.category_id;

-- name: UpdateProduct :one
WITH updated AS (
UPDATE products
SET
  name = COALESCE(sqlc.narg('name_value'), name),
  sku = COALESCE(sqlc.narg('sku_value'), sku),
  description = CASE
    WHEN sqlc.arg('touch_description')::boolean THEN sqlc.narg('description_value')
    ELSE description
  END,
  price = COALESCE(sqlc.narg('price_value'), price),
  stock = COALESCE(sqlc.narg('stock_value'), stock),
  image = CASE
    WHEN sqlc.arg('touch_image')::boolean THEN sqlc.narg('image_value')
    ELSE image
  END,
  category_id = CASE
    WHEN sqlc.narg('category_name')::text IS NULL THEN category_id
    ELSE (
      SELECT categories.id
      FROM categories
      WHERE categories.name = sqlc.narg('category_name')
         OR categories.id = COALESCE(sqlc.narg('category_key'), sqlc.narg('category_name'))
      LIMIT 1
    )
  END,
  min_stock = COALESCE(sqlc.narg('min_stock_value'), min_stock),
  rating = CASE
    WHEN sqlc.arg('touch_rating')::boolean THEN sqlc.narg('rating_value')
    ELSE rating
  END,
  is_best_seller = COALESCE(sqlc.narg('is_best_seller_value'), is_best_seller),
  is_customer_favorite = COALESCE(sqlc.narg('is_customer_favorite_value'), is_customer_favorite),
  badge = CASE
    WHEN sqlc.arg('touch_badge')::boolean THEN sqlc.narg('badge_value')
    ELSE badge
  END,
  updated_at = now()
WHERE products.id = sqlc.arg('product_id')
  AND (
    sqlc.narg('category_name')::text IS NULL
    OR EXISTS (
      SELECT 1
      FROM categories
      WHERE categories.name = sqlc.narg('category_name')
         OR categories.id = COALESCE(sqlc.narg('category_key'), sqlc.narg('category_name'))
    )
  )
RETURNING
  products.id, products.sku, products.name, products.description, products.price, products.stock,
  (SELECT categories.name FROM categories WHERE categories.id = products.category_id) AS category_name,
  products.image,
  COALESCE(products.rating::text, '') AS rating,
  products.is_best_seller, products.is_customer_favorite,
  products.badge, products.min_stock, products.created_at, products.updated_at
)
SELECT
  updated.id, updated.sku, updated.name, updated.description, updated.price, updated.stock,
  updated.category_name, updated.image, updated.rating,
  updated.is_best_seller, updated.is_customer_favorite, updated.badge, updated.min_stock,
  updated.created_at, updated.updated_at
FROM updated;

-- name: DeleteProduct :execrows
DELETE FROM products
WHERE id = sqlc.arg('id');

-- name: ListProductsForOverview :many
SELECT
  p.id, p.name, c.name AS category_name, p.stock, p.min_stock
FROM products p
JOIN categories c ON c.id = p.category_id
ORDER BY p.created_at ASC,
  CASE WHEN p.id ~ '^[0-9]+$' THEN lpad(p.id, 12, '0') ELSE p.id END;
