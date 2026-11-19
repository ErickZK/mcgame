// Open-sourced by BaoLT

// Account entity represents a user account with authentication data.
// Supports Argon2id (primary), legacy bcrypt, and legacy MD5 password hashing
// for Flash and Web client coexistence. Handles account bans with optional
// expiration times. Password compares use constant-time comparison.
package auth

import (
	"crypto/subtle"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const DefaultSecondaryPasswordMD5 = "e10adc3949ba59abbe56e057f20f883e"

func IsBcryptHash(hash string) bool {
	return len(hash) > 4 && hash[0] == '$' && hash[1] == '2'
}

type Account struct {
	ID                uuid.UUID
	Username          string
	PasswordHash      string
	SecondaryPassword string
	Email             string
	VIPLevel          int
	Gold              int
	IsBanned          bool
	BanReason         string
	BanExpiry         *time.Time
	CreatedAt         time.Time
	LastLogin         time.Time
}

func NewAccount(username, password, email string) (*Account, error) {
	hash, err := HashPasswordArgon2id(password)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	return &Account{
		ID:                uuid.New(),
		Username:          username,
		PasswordHash:      hash,
		SecondaryPassword: DefaultSecondaryPasswordMD5,
		Email:             email,
		VIPLevel:          0,
		Gold:              0,
		IsBanned:          false,
		CreatedAt:         now,
		LastLogin:         now,
	}, nil
}

func (a *Account) CheckPassword(password string) bool {
	if a.PasswordHash == "" || password == "" {
		return false
	}
	if IsArgon2idHash(a.PasswordHash) {
		return VerifyPasswordArgon2id(password, a.PasswordHash)
	}
	if IsBcryptHash(a.PasswordHash) {
		err := bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password))
		return err == nil
	}
	return subtle.ConstantTimeCompare([]byte(a.PasswordHash), []byte(password)) == 1
}

func (a *Account) CheckPlaintextPassword(plaintext string) (bool, bool) {
	if a.PasswordHash == "" || plaintext == "" {
		return false, false
	}
	md5Pass := MD5Hex(plaintext)
	if IsArgon2idHash(a.PasswordHash) {
		if VerifyPasswordArgon2id(plaintext, a.PasswordHash) {
			return true, false
		}
		if VerifyPasswordArgon2id(md5Pass, a.PasswordHash) {
			return true, true
		}
		return false, false
	}
	if IsBcryptHash(a.PasswordHash) {
		if bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(plaintext)) == nil {
			return true, true
		}
		if bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(md5Pass)) == nil {
			return true, true
		}
		return false, false
	}
	if subtle.ConstantTimeCompare([]byte(a.PasswordHash), []byte(md5Pass)) == 1 {
		return true, true
	}
	return false, false
}

func (a *Account) CheckSecondaryPassword(passwordMD5 string) bool {
	if passwordMD5 == "" {
		return false
	}
	stored := a.SecondaryPasswordHash()
	return subtle.ConstantTimeCompare([]byte(stored), []byte(passwordMD5)) == 1
}

func (a *Account) SetPassword(password string) error {
	hash, err := HashPasswordArgon2id(password)
	if err != nil {
		return err
	}
	a.PasswordHash = hash
	return nil
}

func (a *Account) SetSecondaryPassword(passwordMD5 string) {
	if passwordMD5 == "" {
		a.SecondaryPassword = DefaultSecondaryPasswordMD5
		return
	}
	a.SecondaryPassword = passwordMD5
}

func (a *Account) SecondaryPasswordHash() string {
	if a.SecondaryPassword == "" {
		return DefaultSecondaryPasswordMD5
	}
	return a.SecondaryPassword
}

func (a *Account) IsBannedNow() bool {
	if !a.IsBanned {
		return false
	}
	if a.BanExpiry == nil {
		return true
	}
	return time.Now().Before(*a.BanExpiry)
}

func (a *Account) Ban(reason string, duration *time.Duration) {
	a.IsBanned = true
	a.BanReason = reason
	if duration != nil {
		expiry := time.Now().Add(*duration)
		a.BanExpiry = &expiry
	} else {
		a.BanExpiry = nil
	}
}

func (a *Account) Unban() {
	a.IsBanned = false
	a.BanReason = ""
	a.BanExpiry = nil
}
