package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

func key32(master string) []byte {
	sum := sha256.Sum256([]byte(master))
	return sum[:]
}

func Encrypt(master, plaintext string) (string, error) {
	block, err := aes.NewCipher(key32(master))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func Decrypt(master, ciphertext string) (string, error) {
	if master == "" {
		return "", errors.New("missing master key")
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key32(master))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, data := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	opened, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", err
	}
	return string(opened), nil
}
