-- Izinkan soft-void (voided_*) dan replaces_id tanpa melanggar append-only.
CREATE OR REPLACE FUNCTION stock_movements_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'stock_movements bersifat append-only dan tidak boleh diubah atau dihapus';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF OLD.voided_at IS NULL
       AND NEW.voided_at IS NOT NULL
       AND NEW.id IS NOT DISTINCT FROM OLD.id
       AND NEW.transaction_number IS NOT DISTINCT FROM OLD.transaction_number
       AND NEW.movement_type IS NOT DISTINCT FROM OLD.movement_type
       AND NEW.product_id IS NOT DISTINCT FROM OLD.product_id
       AND NEW.product_name IS NOT DISTINCT FROM OLD.product_name
       AND NEW.category_name IS NOT DISTINCT FROM OLD.category_name
       AND NEW.supplier_id IS NOT DISTINCT FROM OLD.supplier_id
       AND NEW.supplier_name IS NOT DISTINCT FROM OLD.supplier_name
       AND NEW.unit_code IS NOT DISTINCT FROM OLD.unit_code
       AND NEW.quantity IS NOT DISTINCT FROM OLD.quantity
       AND NEW.stock_before IS NOT DISTINCT FROM OLD.stock_before
       AND NEW.stock_after IS NOT DISTINCT FROM OLD.stock_after
       AND NEW.purchase_price IS NOT DISTINCT FROM OLD.purchase_price
       AND NEW.reason IS NOT DISTINCT FROM OLD.reason
       AND NEW.note IS NOT DISTINCT FROM OLD.note
       AND NEW.officer_id IS NOT DISTINCT FROM OLD.officer_id
       AND NEW.officer_name IS NOT DISTINCT FROM OLD.officer_name
       AND NEW.order_id IS NOT DISTINCT FROM OLD.order_id
       AND NEW.created_at IS NOT DISTINCT FROM OLD.created_at
    THEN
      RETURN NEW;
    END IF;
    IF OLD.replaces_id IS NULL
       AND NEW.replaces_id IS NOT NULL
       AND NEW.voided_at IS NOT DISTINCT FROM OLD.voided_at
       AND NEW.id IS NOT DISTINCT FROM OLD.id
       AND NEW.transaction_number IS NOT DISTINCT FROM OLD.transaction_number
       AND NEW.quantity IS NOT DISTINCT FROM OLD.quantity
       AND NEW.stock_before IS NOT DISTINCT FROM OLD.stock_before
       AND NEW.stock_after IS NOT DISTINCT FROM OLD.stock_after
    THEN
      RETURN NEW;
    END IF;
    RAISE EXCEPTION 'stock_movements bersifat append-only dan tidak boleh diubah atau dihapus';
  END IF;
  RETURN NEW;
END;
$$;
