package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	passwordFileVersion = 1
	minPasswordRunes    = 8
	maxPasswordBytes    = 1024
)

var ErrPasswordAlreadyInitialized = errors.New("management password is already initialized")

type passwordParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultPasswordParams = passwordParams{
	memory:      32 * 1024,
	iterations:  3,
	parallelism: 2,
	saltLength:  16,
	keyLength:   32,
}

type passwordFile struct {
	Version      int    `json:"version"`
	PasswordHash string `json:"password_hash"`
}

// PasswordAuth stores and verifies the desktop control-plane password. Only an
// Argon2id hash is persisted; plaintext passwords never leave the request that
// is currently setting or verifying them.
type PasswordAuth struct {
	mu          sync.RWMutex
	path        string
	encodedHash string
	params      passwordParams
}

func NewPasswordAuth(path string) (*PasswordAuth, error) {
	if path == "" {
		return nil, errors.New("management password path is required")
	}
	store := &PasswordAuth{path: path, params: defaultPasswordParams}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read management password: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("protect management password: %w", err)
	}
	var stored passwordFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("decode management password: %w", err)
	}
	if stored.Version != passwordFileVersion || stored.PasswordHash == "" {
		return nil, errors.New("management password file is invalid")
	}
	if _, _, _, err := decodePasswordHash(stored.PasswordHash); err != nil {
		return nil, fmt.Errorf("validate management password hash: %w", err)
	}
	store.encodedHash = stored.PasswordHash
	return store, nil
}

func (p *PasswordAuth) Initialized() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.encodedHash != ""
}

func (p *PasswordAuth) SetInitial(password string) error {
	if p == nil {
		return errors.New("management password is unavailable")
	}
	if err := validateManagementPassword(password); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.encodedHash != "" {
		return ErrPasswordAlreadyInitialized
	}
	encoded, err := encodePasswordHash(password, p.params)
	if err != nil {
		return err
	}
	if err := writePasswordFile(p.path, passwordFile{
		Version:      passwordFileVersion,
		PasswordHash: encoded,
	}); err != nil {
		return err
	}
	p.encodedHash = encoded
	return nil
}

func (p *PasswordAuth) Verify(password string) bool {
	if p == nil || len(password) > maxPasswordBytes {
		return false
	}
	p.mu.RLock()
	encoded := p.encodedHash
	p.mu.RUnlock()
	if encoded == "" {
		return false
	}
	params, salt, expected, err := decodePasswordHash(encoded)
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, params.keyLength)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func validateManagementPassword(password string) error {
	switch {
	case !utf8.ValidString(password):
		return errors.New("management password must be valid UTF-8")
	case utf8.RuneCountInString(password) < minPasswordRunes:
		return fmt.Errorf("management password must be at least %d characters", minPasswordRunes)
	case len(password) > maxPasswordBytes:
		return fmt.Errorf("management password must not exceed %d bytes", maxPasswordBytes)
	default:
		return nil
	}
}

func encodePasswordHash(password string, params passwordParams) (string, error) {
	salt := make([]byte, params.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate management password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, params.keyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		params.memory,
		params.iterations,
		params.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func decodePasswordHash(encoded string) (passwordParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return passwordParams{}, nil, nil, errors.New("unsupported management password hash")
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return passwordParams{}, nil, nil, errors.New("unsupported Argon2 version")
	}
	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 ||
		!strings.HasPrefix(parameters[0], "m=") ||
		!strings.HasPrefix(parameters[1], "t=") ||
		!strings.HasPrefix(parameters[2], "p=") {
		return passwordParams{}, nil, nil, errors.New("invalid Argon2 parameters")
	}
	memory64, memoryErr := strconv.ParseUint(strings.TrimPrefix(parameters[0], "m="), 10, 32)
	iterations64, iterationsErr := strconv.ParseUint(strings.TrimPrefix(parameters[1], "t="), 10, 32)
	parallelism64, parallelismErr := strconv.ParseUint(strings.TrimPrefix(parameters[2], "p="), 10, 8)
	if memoryErr != nil || iterationsErr != nil || parallelismErr != nil {
		return passwordParams{}, nil, nil, errors.New("invalid Argon2 parameters")
	}
	memory := uint32(memory64)
	iterations := uint32(iterations64)
	parallelism := uint8(parallelism64)
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return passwordParams{}, nil, nil, errors.New("invalid Argon2 salt")
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) < 16 {
		return passwordParams{}, nil, nil, errors.New("invalid Argon2 hash")
	}
	if memory < 8*1024 || memory > 256*1024 || iterations == 0 || iterations > 10 || parallelism == 0 || parallelism > 16 {
		return passwordParams{}, nil, nil, errors.New("unsafe Argon2 parameters")
	}
	return passwordParams{
		memory:      memory,
		iterations:  iterations,
		parallelism: parallelism,
		saltLength:  uint32(len(salt)),
		keyLength:   uint32(len(hash)),
	}, salt, hash, nil
}

func writePasswordFile(path string, stored passwordFile) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create management password directory: %w", err)
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode management password: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(parent, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create management password file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect management password file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write management password file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync management password file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close management password file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install management password file: %w", err)
	}
	return nil
}
