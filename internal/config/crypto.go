package config

import (
	"crypto/rand"
	"encoding/hex"

	"golang.org/x/crypto/bcrypt"
)

func generateAPIKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "sk-" + hex.EncodeToString(b)
}

func hashKey(key string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}
