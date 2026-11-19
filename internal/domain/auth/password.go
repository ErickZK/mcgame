// Password hashing and verification supporting Argon2id (primary for web and
// migrated accounts), legacy bcrypt, and legacy MD5 hashes for Flash client
// coexistence. Uses OWASP-recommended Argon2id parameters (64 MiB, 3 iters, 2 threads).
package auth

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argon2MemoryKiB uint32 = 64 * 1024
	argon2Time      uint32 = 3
	argon2Threads   uint8  = 2
	argon2KeyLen    uint32 = 32
	argon2SaltLen          = 16
)

func IsArgon2idHash(hash string) bool {
	return strings.HasPrefix(hash, "$argon2id$")
}

func MD5Hex(input string) string {
	sum := md5.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}

func HashPasswordArgon2id(password string) (string, error) {
	if password == "" {
		return "", errors.New("empty password")
	}
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate argon2id salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2MemoryKiB, argon2Threads, argon2KeyLen)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Key := base64.RawStdEncoding.EncodeToString(key)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2MemoryKiB, argon2Time, argon2Threads, b64Salt, b64Key), nil
}

func VerifyPasswordArgon2id(password string, encoded string) bool {
	if password == "" || !IsArgon2idHash(encoded) {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, iterations uint32
	var threads uint8
	for _, kv := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(kv, "=", 2)
		if len(pair) != 2 {
			return false
		}
		n, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return false
		}
		switch pair[0] {
		case "m":
			memory = uint32(n)
		case "t":
			iterations = uint32(n)
		case "p":
			threads = uint8(n)
		default:
			return false
		}
	}
	if memory == 0 || iterations == 0 || threads == 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return false
	}
	derived := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(derived, expected) == 1
}
