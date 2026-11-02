package store

import "strings"

// Menu keys yang dapat diatur Admin.
var AllMenuKeys = []string{
	"dashboard",
	"pos",
	"orders",
	"products",
	"categories",
	"stock_store",
	"stock_request",
	"stock_warehouse",
	"stock_warehouse_recap",
	"stock_damage",
	"sales_report",
	"stock_report",
	"users",
	"settings",
}

var validMenus = map[string]struct{}{}

func init() {
	for _, key := range AllMenuKeys {
		validMenus[key] = struct{}{}
	}
}

func DefaultMenuAccess(role string) []string {
	switch role {
	case "admin":
		return append([]string{}, AllMenuKeys...)
	case "manager":
		return []string{"dashboard", "orders", "sales_report", "stock_report", "settings"}
	case "finance":
		return []string{"orders", "sales_report", "settings"}
	case "supervisor":
		return []string{"dashboard", "pos", "orders", "products", "stock_store", "stock_request", "stock_damage", "settings"}
	case "pic_gudang":
		return []string{"dashboard", "stock_warehouse", "stock_warehouse_recap", "stock_damage", "stock_request", "settings"}
	case "cashier":
		return []string{"dashboard", "pos", "orders", "products", "stock_store"}
	default:
		return []string{"dashboard"}
	}
}

func NormalizeMenuAccess(role string, access []string) []string {
	if len(access) == 0 {
		return DefaultMenuAccess(role)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(access))
	for _, raw := range access {
		key := strings.TrimSpace(raw)
		if _, ok := validMenus[key]; !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if len(out) == 0 {
		return DefaultMenuAccess(role)
	}
	return out
}

func HasMenu(access []string, key string) bool {
	for _, item := range access {
		if item == key {
			return true
		}
	}
	return false
}
