-- HALF POS — skema relasional target
-- Mesin: PostgreSQL 14+
--
-- Sumber struktur: src/types/index.ts, src/lib/db.ts, src/lib/mock-data.ts,
-- src/lib/stock-data.ts, src/lib/bakery-data.ts, src/lib/promo/promo.ts,
-- src/lib/payment.ts, src/app/api/orders/route.ts, src/app/api/stock/route.ts.
--
-- Dipakai oleh src/lib/db.ts melalui DATABASE_URL.
-- Terapkan dengan npm run db:setup pada database kosong.
-- File ini tidak berisi DROP, TRUNCATE, atau DELETE.

BEGIN;

-- Uang disimpan dalam rupiah utuh. Jangan gunakan REAL/DOUBLE PRECISION.
-- NUMERIC(14,0) menolak pecahan sen.

-- ---------------------------------------------------------------------------
-- Master
-- ---------------------------------------------------------------------------

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  name VARCHAR(100) NOT NULL,
  username VARCHAR(30) NOT NULL,
  email VARCHAR(254) NOT NULL,
  phone VARCHAR(30),
  role TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  avatar TEXT,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_login TIMESTAMPTZ,
  CONSTRAINT users_name_check CHECK (char_length(btrim(name)) >= 1),
  CONSTRAINT users_username_check CHECK (username ~ '^[A-Za-z0-9._-]{3,30}$'),
  CONSTRAINT users_email_check CHECK (
    email ~* '^[^[:space:]@]+@[^[:space:]@]+[.][^[:space:]@]+$'
  ),
  CONSTRAINT users_phone_check CHECK (
    phone IS NULL OR phone ~ '^[0-9+[:space:]-]{1,30}$'
  ),
  CONSTRAINT users_role_check CHECK (role IN ('admin', 'finance', 'cashier', 'manager')),
  CONSTRAINT users_status_check CHECK (status IN ('active', 'inactive')),
  CONSTRAINT users_password_hash_check CHECK (char_length(password_hash) >= 8)
);

COMMENT ON COLUMN users.password_hash IS
  'Hash password (bcrypt atau argon2). Jangan simpan password plaintext.';
COMMENT ON COLUMN users.role IS
  'manager dipertahankan untuk data lama dan diperlakukan setara admin di RBAC.';

CREATE UNIQUE INDEX users_username_lower_uidx ON users (lower(username));
CREATE UNIQUE INDEX users_email_lower_uidx ON users (lower(email));
CREATE INDEX users_role_status_idx ON users (role, status);
CREATE INDEX users_created_at_idx ON users (created_at DESC);

CREATE TABLE user_activities (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  action TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT user_activities_action_check CHECK (char_length(btrim(action)) >= 1),
  CONSTRAINT user_activities_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users (id)
    ON DELETE CASCADE
    ON UPDATE NO ACTION
);

CREATE INDEX user_activities_user_created_idx
  ON user_activities (user_id, created_at DESC);

CREATE TABLE categories (
  id TEXT PRIMARY KEY,
  name VARCHAR(50) NOT NULL UNIQUE,
  sort_order INTEGER NOT NULL,
  CONSTRAINT categories_name_check CHECK (char_length(btrim(name)) >= 1),
  CONSTRAINT categories_sort_order_check CHECK (sort_order >= 0),
  CONSTRAINT categories_sort_order_uidx UNIQUE (sort_order)
);

CREATE TABLE suppliers (
  id TEXT PRIMARY KEY,
  name VARCHAR(120) NOT NULL UNIQUE,
  CONSTRAINT suppliers_name_check CHECK (char_length(btrim(name)) >= 1)
);

CREATE TABLE units (
  code VARCHAR(20) PRIMARY KEY,
  CONSTRAINT units_code_check CHECK (code ~ '^[a-z]{1,20}$')
);

CREATE TABLE products (
  id TEXT PRIMARY KEY,
  sku VARCHAR(20) NOT NULL UNIQUE,
  name VARCHAR(100) NOT NULL,
  description VARCHAR(500),
  price NUMERIC(14, 0) NOT NULL,
  stock INTEGER NOT NULL DEFAULT 0,
  category_id TEXT NOT NULL,
  image TEXT,
  rating NUMERIC(2, 1),
  is_best_seller BOOLEAN NOT NULL DEFAULT FALSE,
  is_customer_favorite BOOLEAN NOT NULL DEFAULT FALSE,
  badge TEXT,
  min_stock INTEGER NOT NULL DEFAULT 10,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT products_sku_check CHECK (char_length(btrim(sku)) BETWEEN 1 AND 20),
  CONSTRAINT products_name_check CHECK (char_length(btrim(name)) >= 1),
  CONSTRAINT products_price_check CHECK (price >= 0),
  CONSTRAINT products_stock_check CHECK (stock >= 0),
  CONSTRAINT products_rating_check CHECK (
    rating IS NULL OR (rating >= 0 AND rating <= 5)
  ),
  CONSTRAINT products_badge_check CHECK (
    badge IS NULL OR badge IN ('new', 'limited')
  ),
  CONSTRAINT products_min_stock_check CHECK (min_stock >= 0),
  CONSTRAINT products_category_id_fkey
    FOREIGN KEY (category_id) REFERENCES categories (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION
);

COMMENT ON COLUMN products.stock IS
  'Saldo operasional kasir. Bukan jumlah yang dihitung ulang dari stock_movements, karena stok awal produk tidak selalu punya baris pergerakan.';
COMMENT ON COLUMN products.badge IS
  'Label etalase. NULL bila produk tidak memakai badge new/limited.';

CREATE INDEX products_category_id_idx ON products (category_id);
CREATE INDEX products_name_lower_idx ON products (lower(name));
CREATE INDEX products_created_at_idx ON products (created_at);

-- ---------------------------------------------------------------------------
-- Promo bundling (bukan baris products)
-- ---------------------------------------------------------------------------

CREATE TABLE promo_bundles (
  id TEXT PRIMARY KEY,
  sku VARCHAR(20) NOT NULL UNIQUE,
  name VARCHAR(100) NOT NULL,
  description TEXT NOT NULL,
  price NUMERIC(14, 0) NOT NULL,
  original_price NUMERIC(14, 0) NOT NULL,
  savings NUMERIC(14, 0) GENERATED ALWAYS AS (original_price - price) STORED,
  promo_label VARCHAR(40) NOT NULL,
  image TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT promo_bundles_sku_check CHECK (char_length(btrim(sku)) BETWEEN 1 AND 20),
  CONSTRAINT promo_bundles_name_check CHECK (char_length(btrim(name)) >= 1),
  CONSTRAINT promo_bundles_price_check CHECK (price >= 0),
  CONSTRAINT promo_bundles_original_price_check CHECK (original_price >= price),
  CONSTRAINT promo_bundles_label_check CHECK (char_length(btrim(promo_label)) >= 1)
);

COMMENT ON TABLE promo_bundles IS
  'Paket promo kasir. Stok tidak disimpan di sini; ketersediaan dihitung dari stok komponen.';

CREATE TABLE promo_bundle_components (
  bundle_id TEXT NOT NULL,
  product_id TEXT NOT NULL,
  quantity INTEGER NOT NULL,
  sort_order INTEGER NOT NULL,
  PRIMARY KEY (bundle_id, product_id),
  CONSTRAINT promo_bundle_components_quantity_check CHECK (quantity > 0),
  CONSTRAINT promo_bundle_components_sort_order_check CHECK (sort_order >= 0),
  CONSTRAINT promo_bundle_components_sort_uidx UNIQUE (bundle_id, sort_order),
  CONSTRAINT promo_bundle_components_bundle_id_fkey
    FOREIGN KEY (bundle_id) REFERENCES promo_bundles (id)
    ON DELETE CASCADE
    ON UPDATE NO ACTION,
  CONSTRAINT promo_bundle_components_product_id_fkey
    FOREIGN KEY (product_id) REFERENCES products (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION
);

CREATE INDEX promo_bundle_components_product_id_idx
  ON promo_bundle_components (product_id);

CREATE TABLE promo_bundle_labels (
  bundle_id TEXT NOT NULL,
  sort_order INTEGER NOT NULL,
  label VARCHAR(80) NOT NULL,
  PRIMARY KEY (bundle_id, sort_order),
  CONSTRAINT promo_bundle_labels_sort_order_check CHECK (sort_order >= 0),
  CONSTRAINT promo_bundle_labels_label_check CHECK (char_length(btrim(label)) >= 1),
  CONSTRAINT promo_bundle_labels_bundle_id_fkey
    FOREIGN KEY (bundle_id) REFERENCES promo_bundles (id)
    ON DELETE CASCADE
    ON UPDATE NO ACTION
);

-- ---------------------------------------------------------------------------
-- Promo landing + kasir (satu baris untuk satu slug)
-- ---------------------------------------------------------------------------

CREATE TABLE promos (
  id TEXT PRIMARY KEY,
  slug VARCHAR(80) NOT NULL UNIQUE,
  name TEXT NOT NULL,
  short_name TEXT NOT NULL,
  description TEXT NOT NULL,
  landing_description TEXT,
  badge TEXT NOT NULL,
  kind TEXT NOT NULL,
  discount_percent INTEGER,
  buy_quantity INTEGER,
  free_quantity INTEGER,
  target_type TEXT NOT NULL,
  category_id TEXT,
  quantity_per_product INTEGER NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  image TEXT,
  gradient TEXT,
  CONSTRAINT promos_slug_check CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  CONSTRAINT promos_name_check CHECK (char_length(btrim(name)) >= 1),
  CONSTRAINT promos_short_name_check CHECK (char_length(btrim(short_name)) >= 1),
  CONSTRAINT promos_badge_check CHECK (char_length(btrim(badge)) >= 1),
  CONSTRAINT promos_kind_check CHECK (kind IN ('percentage', 'buy-get')),
  CONSTRAINT promos_target_type_check CHECK (target_type IN ('products', 'category')),
  CONSTRAINT promos_quantity_check CHECK (quantity_per_product > 0),
  CONSTRAINT promos_kind_fields_check CHECK (
    (
      kind = 'percentage'
      AND discount_percent BETWEEN 1 AND 100
      AND buy_quantity IS NULL
      AND free_quantity IS NULL
    )
    OR
    (
      kind = 'buy-get'
      AND discount_percent IS NULL
      AND buy_quantity > 0
      AND free_quantity > 0
    )
  ),
  CONSTRAINT promos_target_fields_check CHECK (
    (target_type = 'category' AND category_id IS NOT NULL)
    OR
    (target_type = 'products' AND category_id IS NULL)
  ),
  CONSTRAINT promos_category_id_fkey
    FOREIGN KEY (category_id) REFERENCES categories (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION
);

COMMENT ON COLUMN promos.description IS
  'Teks operasional kasir dari PROMO_CONFIGS.';
COMMENT ON COLUMN promos.landing_description IS
  'Teks kartu landing bila berbeda dari description. NULL bila sama.';

CREATE INDEX promos_category_id_idx ON promos (category_id);
CREATE INDEX promos_expires_at_idx ON promos (expires_at);

CREATE TABLE promo_products (
  promo_id TEXT NOT NULL,
  product_id TEXT NOT NULL,
  PRIMARY KEY (promo_id, product_id),
  CONSTRAINT promo_products_promo_id_fkey
    FOREIGN KEY (promo_id) REFERENCES promos (id)
    ON DELETE CASCADE
    ON UPDATE NO ACTION,
  CONSTRAINT promo_products_product_id_fkey
    FOREIGN KEY (product_id) REFERENCES products (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION
);

CREATE INDEX promo_products_product_id_idx ON promo_products (product_id);

-- ---------------------------------------------------------------------------
-- Penjualan
-- ---------------------------------------------------------------------------

CREATE TABLE orders (
  id TEXT PRIMARY KEY,
  order_number VARCHAR(40) NOT NULL UNIQUE,
  subtotal NUMERIC(14, 0) NOT NULL,
  tax NUMERIC(14, 0) NOT NULL,
  discount NUMERIC(14, 0) NOT NULL,
  total NUMERIC(14, 0) NOT NULL,
  payment_method TEXT NOT NULL,
  status TEXT NOT NULL,
  cashier_id TEXT NOT NULL,
  cashier_name VARCHAR(100) NOT NULL,
  customer_name TEXT,
  notes TEXT,
  cash_received NUMERIC(14, 0),
  change_amount NUMERIC(14, 0) NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT orders_number_check CHECK (order_number ~ '^INV-[0-9]{8}-[0-9]{3,}$'),
  CONSTRAINT orders_subtotal_check CHECK (subtotal >= 0),
  CONSTRAINT orders_tax_check CHECK (tax >= 0),
  CONSTRAINT orders_discount_check CHECK (discount >= 0),
  CONSTRAINT orders_total_check CHECK (total >= 0),
  CONSTRAINT orders_total_formula_check CHECK (
    total = GREATEST(subtotal + tax - discount, 0)
  ),
  CONSTRAINT orders_payment_method_check CHECK (
    payment_method IN ('cash', 'debit', 'credit', 'card', 'qris', 'transfer')
  ),
  CONSTRAINT orders_status_check CHECK (
    status IN ('pending', 'completed', 'cancelled', 'refunded')
  ),
  CONSTRAINT orders_cashier_name_check CHECK (char_length(btrim(cashier_name)) >= 1),
  CONSTRAINT orders_cash_received_check CHECK (
    cash_received IS NULL OR cash_received >= 0
  ),
  CONSTRAINT orders_change_amount_check CHECK (change_amount >= 0),
  CONSTRAINT orders_cashier_id_fkey
    FOREIGN KEY (cashier_id) REFERENCES users (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION
);

COMMENT ON COLUMN orders.payment_method IS
  'Metode baris pembayaran pertama. card berarti E-Wallet untuk data lama.';
COMMENT ON COLUMN orders.cashier_name IS
  'Salinan nama kasir pada saat transaksi. Tidak ikut berubah bila user diganti namanya.';
COMMENT ON COLUMN orders.tax IS
  'Nominal pajak transaksi. Tarif 10% dihitung aplikasi; nilai historis disimpan apa adanya.';

CREATE INDEX orders_cashier_id_idx ON orders (cashier_id);
CREATE INDEX orders_created_at_idx ON orders (created_at DESC);
CREATE INDEX orders_status_created_idx ON orders (status, created_at DESC);

CREATE TABLE order_items (
  id TEXT PRIMARY KEY,
  order_id TEXT NOT NULL,
  product_id TEXT,
  bundle_id TEXT,
  product_name VARCHAR(100) NOT NULL,
  quantity INTEGER NOT NULL,
  unit_price NUMERIC(14, 0) NOT NULL,
  subtotal NUMERIC(14, 0) NOT NULL,
  sort_order INTEGER NOT NULL,
  CONSTRAINT order_items_name_check CHECK (char_length(btrim(product_name)) >= 1),
  CONSTRAINT order_items_quantity_check CHECK (quantity > 0),
  CONSTRAINT order_items_unit_price_check CHECK (unit_price >= 0),
  CONSTRAINT order_items_subtotal_check CHECK (subtotal >= 0),
  CONSTRAINT order_items_subtotal_formula_check CHECK (
    subtotal = quantity * unit_price
  ),
  CONSTRAINT order_items_sort_order_check CHECK (sort_order >= 0),
  CONSTRAINT order_items_target_check CHECK (
    (product_id IS NOT NULL AND bundle_id IS NULL)
    OR
    (product_id IS NULL AND bundle_id IS NOT NULL)
  ),
  CONSTRAINT order_items_order_sort_uidx UNIQUE (order_id, sort_order),
  CONSTRAINT order_items_order_id_fkey
    FOREIGN KEY (order_id) REFERENCES orders (id)
    ON DELETE CASCADE
    ON UPDATE NO ACTION,
  CONSTRAINT order_items_product_id_fkey
    FOREIGN KEY (product_id) REFERENCES products (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION,
  CONSTRAINT order_items_bundle_id_fkey
    FOREIGN KEY (bundle_id) REFERENCES promo_bundles (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION
);

-- (order_id, sort_order) sudah unik, jadi order_id tidak perlu indeks kedua.
CREATE INDEX order_items_product_id_idx ON order_items (product_id);
CREATE INDEX order_items_bundle_id_idx ON order_items (bundle_id);

CREATE TABLE order_item_components (
  order_item_id TEXT NOT NULL,
  product_id TEXT NOT NULL,
  product_name VARCHAR(100) NOT NULL,
  quantity INTEGER NOT NULL,
  PRIMARY KEY (order_item_id, product_id),
  CONSTRAINT order_item_components_name_check CHECK (char_length(btrim(product_name)) >= 1),
  CONSTRAINT order_item_components_quantity_check CHECK (quantity > 0),
  CONSTRAINT order_item_components_order_item_id_fkey
    FOREIGN KEY (order_item_id) REFERENCES order_items (id)
    ON DELETE CASCADE
    ON UPDATE NO ACTION,
  CONSTRAINT order_item_components_product_id_fkey
    FOREIGN KEY (product_id) REFERENCES products (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION
);

COMMENT ON COLUMN order_item_components.quantity IS
  'Jumlah komponen untuk satu paket. Konsumsi stok = quantity * order_items.quantity.';

CREATE INDEX order_item_components_product_id_idx
  ON order_item_components (product_id);

CREATE TABLE order_payments (
  id TEXT PRIMARY KEY,
  order_id TEXT NOT NULL,
  sort_order INTEGER NOT NULL,
  method TEXT NOT NULL,
  amount NUMERIC(14, 0) NOT NULL,
  cash_received NUMERIC(14, 0),
  CONSTRAINT order_payments_sort_order_check CHECK (sort_order >= 0),
  CONSTRAINT order_payments_method_check CHECK (
    method IN ('cash', 'debit', 'credit', 'card', 'qris', 'transfer')
  ),
  CONSTRAINT order_payments_amount_check CHECK (amount > 0),
  CONSTRAINT order_payments_cash_check CHECK (
    (
      method = 'cash'
      AND cash_received IS NOT NULL
      AND cash_received >= amount
    )
    OR
    (
      method <> 'cash'
      AND cash_received IS NULL
    )
  ),
  CONSTRAINT order_payments_order_sort_uidx UNIQUE (order_id, sort_order),
  CONSTRAINT order_payments_order_id_fkey
    FOREIGN KEY (order_id) REFERENCES orders (id)
    ON DELETE CASCADE
    ON UPDATE NO ACTION
);

COMMENT ON TABLE order_payments IS
  'Satu transaksi boleh punya beberapa baris (split payment). Transaksi lama boleh belum punya baris ini.';

-- (order_id, sort_order) sudah unik, jadi order_id tidak perlu indeks kedua.

-- ---------------------------------------------------------------------------
-- Audit stok (append-only)
-- ---------------------------------------------------------------------------

CREATE TABLE stock_movements (
  id TEXT PRIMARY KEY,
  transaction_number VARCHAR(40) NOT NULL UNIQUE,
  movement_type TEXT NOT NULL,
  product_id TEXT NOT NULL,
  product_name VARCHAR(100) NOT NULL,
  category_name VARCHAR(50) NOT NULL,
  supplier_id TEXT,
  supplier_name VARCHAR(120),
  unit_code VARCHAR(20),
  quantity INTEGER NOT NULL,
  stock_before INTEGER NOT NULL,
  stock_after INTEGER NOT NULL,
  purchase_price NUMERIC(14, 0),
  total_price NUMERIC(14, 0) GENERATED ALWAYS AS (
    CASE
      WHEN purchase_price IS NULL THEN NULL
      ELSE purchase_price * quantity
    END
  ) STORED,
  reason TEXT,
  note TEXT,
  officer_id TEXT NOT NULL,
  officer_name VARCHAR(100) NOT NULL,
  order_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT stock_movements_number_check CHECK (
    (movement_type = 'in' AND transaction_number ~ '^STI-[0-9]{8}-[0-9]{3,}$')
    OR
    (movement_type = 'out' AND transaction_number ~ '^STO-[0-9]{8}-[0-9]{3,}$')
  ),
  CONSTRAINT stock_movements_type_check CHECK (movement_type IN ('in', 'out')),
  CONSTRAINT stock_movements_product_name_check CHECK (char_length(btrim(product_name)) >= 1),
  CONSTRAINT stock_movements_category_name_check CHECK (char_length(btrim(category_name)) >= 1),
  CONSTRAINT stock_movements_quantity_check CHECK (quantity > 0),
  CONSTRAINT stock_movements_stock_before_check CHECK (stock_before >= 0),
  CONSTRAINT stock_movements_stock_after_check CHECK (stock_after >= 0),
  CONSTRAINT stock_movements_balance_check CHECK (
    (movement_type = 'in' AND stock_after = stock_before + quantity)
    OR
    (movement_type = 'out' AND stock_after = stock_before - quantity)
  ),
  CONSTRAINT stock_movements_purchase_price_check CHECK (
    purchase_price IS NULL OR purchase_price >= 0
  ),
  CONSTRAINT stock_movements_reason_check CHECK (
    reason IS NULL
    OR reason IN (
      'Penjualan',
      'Produk Rusak',
      'Kadaluarsa',
      'Sampel',
      'Retur',
      'Lainnya'
    )
  ),
  CONSTRAINT stock_movements_reason_required_check CHECK (
    movement_type = 'in' OR reason IS NOT NULL
  ),
  CONSTRAINT stock_movements_supplier_required_check CHECK (
    movement_type = 'out' OR supplier_id IS NOT NULL
  ),
  CONSTRAINT stock_movements_supplier_pair_check CHECK (
    (supplier_id IS NULL AND supplier_name IS NULL)
    OR
    (supplier_id IS NOT NULL AND supplier_name IS NOT NULL)
  ),
  CONSTRAINT stock_movements_officer_name_check CHECK (char_length(btrim(officer_name)) >= 1),
  CONSTRAINT stock_movements_product_id_fkey
    FOREIGN KEY (product_id) REFERENCES products (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION,
  CONSTRAINT stock_movements_supplier_id_fkey
    FOREIGN KEY (supplier_id) REFERENCES suppliers (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION,
  CONSTRAINT stock_movements_unit_code_fkey
    FOREIGN KEY (unit_code) REFERENCES units (code)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION,
  CONSTRAINT stock_movements_officer_id_fkey
    FOREIGN KEY (officer_id) REFERENCES users (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION,
  CONSTRAINT stock_movements_order_id_fkey
    FOREIGN KEY (order_id) REFERENCES orders (id)
    ON DELETE RESTRICT
    ON UPDATE NO ACTION
);

COMMENT ON COLUMN stock_movements.product_name IS
  'Salinan nama produk saat pergerakan dicatat.';
COMMENT ON COLUMN stock_movements.order_id IS
  'Diisi bila pergerakan berasal dari penjualan. Penyimpanan in-memory saat ini hanya menulis nomor invoice ke note.';

CREATE INDEX stock_movements_product_created_idx
  ON stock_movements (product_id, created_at DESC);
CREATE INDEX stock_movements_created_at_idx
  ON stock_movements (created_at DESC);
CREATE INDEX stock_movements_officer_id_idx ON stock_movements (officer_id);
CREATE INDEX stock_movements_supplier_id_idx ON stock_movements (supplier_id);
CREATE INDEX stock_movements_order_id_idx ON stock_movements (order_id);

CREATE FUNCTION stock_movements_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'stock_movements bersifat append-only dan tidak boleh diubah atau dihapus';
END;
$$;

CREATE TRIGGER stock_movements_append_only_trg
  BEFORE UPDATE OR DELETE ON stock_movements
  FOR EACH ROW
  EXECUTE FUNCTION stock_movements_append_only();

-- ---------------------------------------------------------------------------
-- Nomor dokumen O(1). Baris ini dikunci per prefix, bukan memindai invoice.
-- ---------------------------------------------------------------------------

CREATE TABLE document_sequences (
  prefix TEXT PRIMARY KEY,
  last_value INTEGER NOT NULL,
  CONSTRAINT document_sequences_last_value_check CHECK (last_value >= 0)
);

COMMENT ON TABLE document_sequences IS
  'Penghitung nomor invoice dan mutasi stok per prefix harian.';

CREATE FUNCTION next_document_numbers(p_prefix TEXT, p_count INTEGER)
RETURNS INTEGER
LANGUAGE plpgsql
SET search_path = public
AS $$
DECLARE
  current_value INTEGER;
  observed INTEGER := 0;
  upper_bound TEXT;
BEGIN
  IF p_count IS NULL OR p_count < 1 THEN
    RAISE EXCEPTION 'jumlah nomor harus positif';
  END IF;
  IF p_prefix IS NULL OR char_length(p_prefix) < 5 THEN
    RAISE EXCEPTION 'prefix nomor dokumen tidak valid';
  END IF;

  PERFORM pg_advisory_xact_lock(hashtext('halfpos-doc'), hashtext(p_prefix));

  SELECT last_value
  INTO current_value
  FROM document_sequences
  WHERE prefix = p_prefix
  FOR UPDATE;

  IF NOT FOUND THEN
    upper_bound := left(p_prefix, char_length(p_prefix) - 1)
      || chr(ascii(right(p_prefix, 1)) + 1);

    IF p_prefix LIKE 'INV-%' THEN
      SELECT COALESCE(MAX(substring(order_number FROM char_length(p_prefix) + 1)::INTEGER), 0)
      INTO observed
      FROM orders
      WHERE order_number >= p_prefix
        AND order_number < upper_bound
        AND substring(order_number FROM char_length(p_prefix) + 1) ~ '^[0-9]+$';
    ELSIF p_prefix LIKE 'STI-%' OR p_prefix LIKE 'STO-%' THEN
      SELECT COALESCE(MAX(substring(transaction_number FROM char_length(p_prefix) + 1)::INTEGER), 0)
      INTO observed
      FROM stock_movements
      WHERE transaction_number >= p_prefix
        AND transaction_number < upper_bound
        AND substring(transaction_number FROM char_length(p_prefix) + 1) ~ '^[0-9]+$';
    END IF;

    INSERT INTO document_sequences (prefix, last_value)
    VALUES (p_prefix, observed);
    current_value := observed;
  END IF;

  UPDATE document_sequences
  SET last_value = current_value + p_count
  WHERE prefix = p_prefix
  RETURNING last_value INTO current_value;

  RETURN current_value;
END;
$$;

COMMENT ON FUNCTION next_document_numbers(TEXT, INTEGER) IS
  'Mengembalikan nilai akhir sebuah rentang nomor. Nomor pertama adalah hasil dikurangi jumlah ditambah satu.';

-- ---------------------------------------------------------------------------
-- Data referensi yang sudah ada di kode aplikasi.
-- Bukan data transaksi, user, atau password.
-- ---------------------------------------------------------------------------

INSERT INTO categories (id, name, sort_order) VALUES
  ('cake', 'Cake', 1),
  ('bread', 'Bread', 2),
  ('pastry', 'Pastry', 3),
  ('cookies', 'Cookies', 4),
  ('dessert', 'Dessert', 5),
  ('coffee-drink', 'Coffee & Drink', 6);

INSERT INTO suppliers (id, name) VALUES
  ('sup-tepung-makmur', 'Supplier Tepung Makmur'),
  ('sup-mentega-sejahtera', 'CV Mentega Sejahtera'),
  ('sup-coklat-nusantara', 'Coklat Nusantara'),
  ('sup-dairy-fresh', 'Dairy Fresh Makassar'),
  ('sup-bahan-kue-sentral', 'Toko Bahan Kue Sentral'),
  ('sup-kopi-arabika', 'Distributor Kopi Arabika');

INSERT INTO units (code) VALUES
  ('pcs'),
  ('box'),
  ('kg'),
  ('gram'),
  ('pack'),
  ('lusin'),
  ('liter');

COMMIT;
