package auth

import "testing"

// Hash dihasilkan oleh crypto.scryptSync Node (N=16384, r=8, p=1, keyLen=64).
// Salt adalah string heksadesimal yang dipakai sebagai byte UTF-8.
const existingHash = "scrypt$0123456789abcdef0123456789abcdef$7bfc2ad8e90d7252b24c322d25a3ef314bb6f3b0ad29738a3140d921aa2606024e5f3f7eef2b45dfb16dd5428705bb539af7384b2504308175cc714be9c428bf"

func TestVerifyExistingScryptHash(t *testing.T) {
	if !VerifyPassword("password123", existingHash) {
		t.Fatal("hash scrypt yang sudah ada harus lolos")
	}
	if VerifyPassword("wrong-password", existingHash) {
		t.Fatal("password salah harus ditolak")
	}
	if VerifyPassword("password123", "scrypt$0123456789abcdef0123456789abcdef$abcd") {
		t.Fatal("hash pendek harus ditolak")
	}
	if VerifyPassword("password123", "bcrypt$aa$bb") {
		t.Fatal("skema lain harus ditolak")
	}
}

func TestHashRoundTrip(t *testing.T) {
	stored, err := HashPassword("rahasia-kasir")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("rahasia-kasir", stored) {
		t.Fatal("hash baru harus cocok")
	}
	if VerifyPassword("lain", stored) {
		t.Fatal("password lain harus ditolak")
	}
}
