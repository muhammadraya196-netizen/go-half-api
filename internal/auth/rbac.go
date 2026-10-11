package auth

// Role manager diperlakukan setara admin.

func CanViewUsers(role string) bool {
	switch role {
	case "admin", "manager", "finance":
		return true
	default:
		return false
	}
}

func CanMutateUsers(role string) bool {
	switch role {
	case "admin", "manager":
		return true
	default:
		return false
	}
}
