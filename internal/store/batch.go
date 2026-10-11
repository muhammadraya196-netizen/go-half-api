package store

import "github.com/jackc/pgx/v5"

func productBatch(arg ListProductsParams) *pgx.Batch {
	batch := &pgx.Batch{}
	batch.Queue(listProducts,
		arg.Search, arg.Category, arg.Status, arg.MinPrice, arg.MaxPrice, arg.MinStock,
		arg.RowOffset, arg.RowLimit,
	)
	batch.Queue(countProducts,
		arg.Search, arg.Category, arg.Status, arg.MinPrice, arg.MaxPrice, arg.MinStock,
	)
	return batch
}

func userBatch(arg ListUsersParams) *pgx.Batch {
	batch := &pgx.Batch{}
	batch.Queue(listUsers,
		arg.Search, arg.Role, arg.Status, arg.StartAt, arg.EndBefore,
		arg.CursorCreatedAt, arg.CursorID, arg.RowOffset, arg.RowLimit,
	)
	batch.Queue(countUsers, arg.Search, arg.Role, arg.Status, arg.StartAt, arg.EndBefore)
	return batch
}

func orderBatch(arg ListOrdersParams) *pgx.Batch {
	batch := &pgx.Batch{}
	batch.Queue(listOrders,
		arg.Status, arg.StartAt, arg.EndBefore, arg.Search, arg.CursorCreatedAt,
		arg.SortBy, arg.SortDir, arg.CursorID, arg.RowOffset, arg.RowLimit,
	)
	batch.Queue(countOrders, arg.Status, arg.StartAt, arg.EndBefore, arg.Search)
	return batch
}

func stockBatch(arg ListStockMovementsParams) *pgx.Batch {
	batch := &pgx.Batch{}
	batch.Queue(listStockMovements,
		arg.ProductID, arg.MovementType, arg.Supplier, arg.StartAt, arg.EndBefore,
		arg.Search, arg.CursorCreatedAt, arg.CursorID, arg.RowOffset, arg.RowLimit,
	)
	batch.Queue(countStockMovements,
		arg.ProductID, arg.MovementType, arg.Supplier, arg.StartAt, arg.EndBefore, arg.Search,
	)
	return batch
}
