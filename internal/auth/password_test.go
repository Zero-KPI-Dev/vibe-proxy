package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordAuthInitializesPersistsAndVerifiesWithoutPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop-auth.json")
	store, err := NewPasswordAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Initialized() {
		t.Fatal("new password store was initialized")
	}
	if err := store.SetInitial("short"); err == nil {
		t.Fatal("short management password was accepted")
	}

	const password = "correct horse battery staple"
	if err := store.SetInitial(password); err != nil {
		t.Fatalf("SetInitial() error = %v", err)
	}
	if !store.Initialized() || !store.Verify(password) || store.Verify("wrong password") {
		t.Fatal("password verification result was incorrect")
	}
	if err := store.SetInitial("another valid password"); !errors.Is(err, ErrPasswordAlreadyInitialized) {
		t.Fatalf("second SetInitial() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("password file permissions = %#o, want 0600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), password) || !strings.Contains(string(data), "$argon2id$") {
		t.Fatal("password file exposed plaintext or omitted Argon2id hash")
	}

	reloaded, err := NewPasswordAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Initialized() || !reloaded.Verify(password) {
		t.Fatal("reloaded password store did not verify the password")
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPasswordAuth(path); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("reloaded password file permissions = %#o, want 0600", got)
	}
}

func TestPasswordAuthRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop-auth.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"password_hash":"invalid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPasswordAuth(path); err == nil {
		t.Fatal("malformed management password file was accepted")
	}
}
