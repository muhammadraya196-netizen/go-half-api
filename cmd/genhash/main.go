package main

import (
	"fmt"
	"os"

	"halfpos/internal/auth"
)

func main() {
	pw := "kasir123"
	if len(os.Args) > 1 {
		pw = os.Args[1]
	}
	h, err := auth.HashPassword(pw)
	if err != nil {
		panic(err)
	}
	fmt.Print(h)
}
