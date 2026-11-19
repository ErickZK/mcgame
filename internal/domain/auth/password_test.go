// Unit tests for Argon2id password hashing and legacy bcrypt/MD5 migration paths.
package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashAndVerifyPasswordArgon2id(t *testing.T) {
	hash, err := HashPasswordArgon2id("SuperSecretPass123!")
	if err != nil {
		t.Fatalf("HashPasswordArgon2id error: %v", err)
	}
	if !IsArgon2idHash(hash) {
		t.Fatalf("expected argon2id prefix, got %q", hash)
	}
	if !VerifyPasswordArgon2id("SuperSecretPass123!", hash) {
		t.Fatalf("expected valid password to verify")
	}
	if VerifyPasswordArgon2id("WrongPass", hash) {
		t.Fatalf("expected wrong password to fail")
	}
}

func TestAccountCheckPlaintextPassword_MigrationPaths(t *testing.T) {
	plaintext := "MyHeroPass99"
	md5Val := MD5Hex(plaintext)

	t.Run("legacy MD5 row", func(t *testing.T) {
		acc := &Account{PasswordHash: md5Val}
		ok, upgrade := acc.CheckPlaintextPassword(plaintext)
		if !ok || !upgrade {
			t.Fatalf("expected ok=true upgrade=true, got ok=%v upgrade=%v", ok, upgrade)
		}
	})

	t.Run("legacy bcrypt(MD5) row", func(t *testing.T) {
		bHash, err := bcrypt.GenerateFromPassword([]byte(md5Val), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("bcrypt error: %v", err)
		}
		acc := &Account{PasswordHash: string(bHash)}
		ok, upgrade := acc.CheckPlaintextPassword(plaintext)
		if !ok || !upgrade {
			t.Fatalf("expected ok=true upgrade=true, got ok=%v upgrade=%v", ok, upgrade)
		}
	})

	t.Run("modern argon2id(plaintext) row", func(t *testing.T) {
		acc, err := NewAccount("hero", plaintext, "hero@example.com")
		if err != nil {
			t.Fatalf("NewAccount error: %v", err)
		}
		ok, upgrade := acc.CheckPlaintextPassword(plaintext)
		if !ok || upgrade {
			t.Fatalf("expected ok=true upgrade=false, got ok=%v upgrade=%v", ok, upgrade)
		}
	})
}
