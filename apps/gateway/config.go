package main

import (
	"os"
	"strconv"
)

func getenvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" { return fallback }
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 { return fallback }
	return value
}