package domain

import "time"

type Product struct {
	ID                 string
	SKU                string
	Name               string
	Description        string
	Price              int64
	Stock              int32
	Category           string
	Image              *string
	Rating             *float64
	IsBestSeller       bool
	IsCustomerFavorite bool
	Badge              *string
	MinStock           int32
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ProductFilter struct {
	Search   string
	Category string
	Status   string
	MinPrice *int64
	MaxPrice *int64
	MinStock *int32
	Limit    *int32
	Offset   int32
}

type ProductPage struct {
	Data   []Product
	Total  int64
	Limit  int32
	Offset int32
}

type ProductInput struct {
	Name               *string
	SKU                *string
	Description        *string
	DescriptionSet     bool
	Price              *int64
	Stock              *int32
	Category           *string
	Image              *string
	ImageSet           bool
	Rating             *float64
	RatingSet          bool
	IsBestSeller       *bool
	IsCustomerFavorite *bool
	Badge              *string
	BadgeSet           bool
	MinStock           *int32
}

type ManagedUser struct {
	ID        string
	Name      string
	Username  string
	Email     string
	Phone     *string
	Role      string
	Status    string
	Avatar    *string
	CreatedAt time.Time
	LastLogin *time.Time
}

type UserActivity struct {
	ID        string
	UserID    string
	Action    string
	CreatedAt time.Time
}

type UserFilter struct {
	Search          string
	Role            string
	Status          string
	Start           *time.Time
	EndBefore       *time.Time
	Limit           int32
	Offset          int32
	CursorCreatedAt *time.Time
	CursorID        string
}

type UserPage struct {
	Data   []ManagedUser
	Total  int64
	Limit  int32
	Offset int32
}

type UserDetail struct {
	User       ManagedUser
	Activities []UserActivity
}

type CreateUserInput struct {
	Name     string
	Username string
	Email    string
	Phone    string
	Role     string
	Status   string
	Avatar   string
	Password string
}

type UpdateUserInput struct {
	Name     string
	Username string
	Email    string
	Phone    string
	Role     string
	Status   string
	Avatar   string
}

type AuthUser struct {
	ID    string
	Name  string
	Email string
	Role  string
}

type OrderItem struct {
	ProductID        string
	ProductName      string
	Quantity         int32
	Price            int64
	Subtotal         int64
	BundleComponents []BundleComponent
}

type BundleComponent struct {
	ProductID   string
	ProductName string
	Quantity    int32
}

type OrderPayment struct {
	Method       string
	Amount       int64
	CashReceived *int64
}

type Order struct {
	ID            string
	OrderNumber   string
	Items         []OrderItem
	Subtotal      int64
	Tax           int64
	Discount      int64
	Total         int64
	PaymentMethod string
	Payments      []OrderPayment
	Status        string
	CashierID     string
	CashierName   string
	CustomerName  *string
	Notes         *string
	CashReceived  *int64
	ChangeAmount  int64
	CreatedAt     time.Time
}

type OrderFilter struct {
	Status          string
	Search          string
	Start           *time.Time
	EndBefore       *time.Time
	SortBy          string
	SortDir         string
	Limit           *int32
	Offset          int32
	CursorCreatedAt *time.Time
	CursorID        string
}

type OrderPage struct {
	Data   []Order
	Total  int64
	Limit  int32
	Offset int32
}

type OrderItemInput struct {
	ProductID   string
	ProductName string
	Quantity    int32
	Price       int64
}

type PaymentInput struct {
	Method       string
	Amount       int64
	CashReceived *int64
}

type CreateOrderInput struct {
	Items         []OrderItemInput
	Subtotal      int64
	Tax           int64
	Discount      int64
	Total         int64
	PaymentMethod string
	Payments      []PaymentInput
	CashierID     string
	CashierName   string
	CustomerName  string
	Notes         string
	CashReceived  *int64
	ChangeAmount  int64
}

type StockMovement struct {
	ID                string
	TransactionNumber string
	Type              string
	ProductID         string
	ProductName       string
	Category          string
	Supplier          *string
	Unit              *string
	QuantityIn        int32
	QuantityOut       int32
	StockBefore       int32
	StockAfter        int32
	PurchasePrice     *int64
	TotalPrice        *int64
	Reason            *string
	Note              *string
	OfficerID         string
	OfficerName       string
	CreatedAt         time.Time
}

type StockFilter struct {
	ProductID       string
	Type            string
	Supplier        string
	Search          string
	Start           *time.Time
	EndBefore       *time.Time
	Limit           int32
	Offset          int32
	CursorCreatedAt *time.Time
	CursorID        string
}

type StockPage struct {
	Data   []StockMovement
	Total  int64
	Limit  int32
	Offset int32
}

type StockInput struct {
	Type          string
	ProductID     string
	Supplier      string
	Unit          string
	Quantity      int32
	PurchasePrice *int64
	Reason        string
	Note          string
	OfficerID     string
	OfficerName   string
}

type ProductStockInfo struct {
	ProductID    string
	Name         string
	Category     string
	CurrentStock int32
	TotalIn      int64
	TotalOut     int64
	MinStock     int32
	Status       string
}

type StockSummary struct {
	TotalProducts      int
	TotalStock         int64
	TotalStockInToday  int64
	TotalStockOutToday int64
	LowStockCount      int
	OutOfStockCount    int
}

type StockOverview struct {
	Summary  StockSummary
	Products []ProductStockInfo
}

type Analytics struct {
	TotalRevenue    int64
	TotalOrders     int64
	TotalProducts   int64
	CompletedOrders int64
	TodaySales      int64
	TodayOrderCount int64
	TodayItemsSold  int64
}
