package store

import (
	"context"
	"errors"

	"halfpos/internal/domain"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ListProducts(ctx context.Context, filter domain.ProductFilter) (domain.ProductPage, error) {
	params := ListProductsParams{
		Search:    likePattern(filter.Search),
		Category:  optString(filter.Category),
		Status:    optString(filter.Status),
		MinPrice:  filter.MinPrice,
		MaxPrice:  filter.MaxPrice,
		MinStock:  filter.MinStock,
		RowOffset: filter.Offset,
		RowLimit:  filter.Limit,
	}
	batch := s.pool.SendBatch(ctx, productBatch(params))
	defer batch.Close()

	rows, err := batch.Query()
	if err != nil {
		return domain.ProductPage{}, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[ListProductsRow])
	if err != nil {
		return domain.ProductPage{}, err
	}
	var total int64
	if err := batch.QueryRow().Scan(&total); err != nil {
		return domain.ProductPage{}, err
	}
	out := make([]domain.Product, 0, len(items))
	for _, row := range items {
		out = append(out, mapListedProduct(row))
	}
	limit := int32(0)
	if filter.Limit != nil {
		limit = *filter.Limit
	}
	return domain.ProductPage{Data: out, Total: total, Limit: limit, Offset: filter.Offset}, nil
}

func (s *Store) GetProduct(ctx context.Context, id string) (domain.Product, error) {
	row, err := New(s.pool).GetProduct(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Product{}, NotFound("Produk tidak ditemukan")
	}
	if err != nil {
		return domain.Product{}, err
	}
	return mapGotProduct(row), nil
}

func (s *Store) CreateProduct(ctx context.Context, name, sku, description, category string, price int64, stock int32) (domain.Product, error) {
	row, err := New(s.pool).CreateProduct(ctx, CreateProductParams{
		ID:          newID(""),
		Sku:         sku,
		Name:        name,
		Description: blank(description),
		Price:       price,
		Stock:       stock,
		Category:    category,
		CategoryKey: categoryKey(category),
	})
	if err != nil {
		if mapped := mapUnique(err); mapped != nil {
			return domain.Product{}, mapped
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Product{}, BadRequest("Kategori tidak ditemukan")
		}
		return domain.Product{}, err
	}
	return mapCreatedProduct(row), nil
}

func (s *Store) UpdateProduct(ctx context.Context, id string, in domain.ProductInput) (domain.Product, error) {
	var categoryName, categoryLookup *string
	if in.Category != nil {
		categoryName = in.Category
		key := categoryKey(*in.Category)
		categoryLookup = &key
	}
	row, err := New(s.pool).UpdateProduct(ctx, UpdateProductParams{
		NameValue:               in.Name,
		SkuValue:                in.SKU,
		TouchDescription:        in.DescriptionSet,
		DescriptionValue:        in.Description,
		PriceValue:              in.Price,
		StockValue:              in.Stock,
		TouchImage:              in.ImageSet,
		ImageValue:              in.Image,
		CategoryName:            categoryName,
		CategoryKey:             categoryLookup,
		MinStockValue:           in.MinStock,
		TouchRating:             in.RatingSet,
		RatingValue:             in.Rating,
		IsBestSellerValue:       in.IsBestSeller,
		IsCustomerFavoriteValue: in.IsCustomerFavorite,
		TouchBadge:              in.BadgeSet,
		BadgeValue:              in.Badge,
		ProductID:               id,
	})
	if err != nil {
		if mapped := mapUnique(err); mapped != nil {
			return domain.Product{}, mapped
		}
		if errors.Is(err, pgx.ErrNoRows) {
			if _, getErr := New(s.pool).GetProduct(ctx, id); errors.Is(getErr, pgx.ErrNoRows) {
				return domain.Product{}, NotFound("Produk tidak ditemukan")
			}
			if in.Category != nil {
				return domain.Product{}, BadRequest("Kategori tidak ditemukan")
			}
			return domain.Product{}, NotFound("Produk tidak ditemukan")
		}
		return domain.Product{}, err
	}
	return mapUpdatedProduct(row), nil
}

func (s *Store) DeleteProduct(ctx context.Context, id string) error {
	n, err := New(s.pool).DeleteProduct(ctx, id)
	if err != nil {
		if isForeignKey(err) {
			return Conflict("Produk tidak dapat dihapus karena masih dipakai transaksi, stok, atau promo")
		}
		return err
	}
	if n == 0 {
		return NotFound("Produk tidak ditemukan")
	}
	return nil
}

func mapListedProduct(row ListProductsRow) domain.Product {
	description := ""
	if row.Description != nil {
		description = *row.Description
	}
	return domain.Product{
		ID:                 row.ID,
		SKU:                row.Sku,
		Name:               row.Name,
		Description:        description,
		Price:              row.Price,
		Stock:              row.Stock,
		Category:           row.CategoryName,
		Image:              row.Image,
		Rating:             row.Rating,
		IsBestSeller:       row.IsBestSeller,
		IsCustomerFavorite: row.IsCustomerFavorite,
		Badge:              row.Badge,
		MinStock:           row.MinStock,
		CreatedAt:          tsTime(row.CreatedAt),
		UpdatedAt:          tsTime(row.UpdatedAt),
	}
}

func mapGotProduct(row GetProductRow) domain.Product {
	description := ""
	if row.Description != nil {
		description = *row.Description
	}
	return domain.Product{
		ID: row.ID, SKU: row.Sku, Name: row.Name, Description: description,
		Price: row.Price, Stock: row.Stock, Category: row.CategoryName,
		Image: row.Image, Rating: row.Rating, IsBestSeller: row.IsBestSeller,
		IsCustomerFavorite: row.IsCustomerFavorite, Badge: row.Badge, MinStock: row.MinStock,
		CreatedAt: tsTime(row.CreatedAt), UpdatedAt: tsTime(row.UpdatedAt),
	}
}

func mapCreatedProduct(row CreateProductRow) domain.Product {
	description := ""
	if row.Description != nil {
		description = *row.Description
	}
	return domain.Product{
		ID: row.ID, SKU: row.Sku, Name: row.Name, Description: description,
		Price: row.Price, Stock: row.Stock, Category: row.CategoryName,
		Image: row.Image, Rating: parseRating(row.Rating), IsBestSeller: row.IsBestSeller,
		IsCustomerFavorite: row.IsCustomerFavorite, Badge: row.Badge, MinStock: row.MinStock,
		CreatedAt: tsTime(row.CreatedAt), UpdatedAt: tsTime(row.UpdatedAt),
	}
}

func mapUpdatedProduct(row UpdateProductRow) domain.Product {
	description := ""
	if row.Description != nil {
		description = *row.Description
	}
	return domain.Product{
		ID: row.ID, SKU: row.Sku, Name: row.Name, Description: description,
		Price: row.Price, Stock: row.Stock, Category: row.CategoryName,
		Image: row.Image, Rating: parseRating(row.Rating), IsBestSeller: row.IsBestSeller,
		IsCustomerFavorite: row.IsCustomerFavorite, Badge: row.Badge, MinStock: row.MinStock,
		CreatedAt: tsTime(row.CreatedAt), UpdatedAt: tsTime(row.UpdatedAt),
	}
}
