package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const (
	passwordHashPrefix = "sha256"
	passwordHashIters  = 200000
	passwordSaltBytes  = 16
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := passwordDigest(password, salt, passwordHashIters)
	return fmt.Sprintf("%s$%d$%s$%s",
		passwordHashPrefix,
		passwordHashIters,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(hash),
	), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 {
		return false
	}
	if parts[0] != passwordHashPrefix {
		return false
	}
	iters, err := strconv.Atoi(parts[1])
	if err != nil || iters <= 0 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual := passwordDigest(password, salt, iters)
	return subtleCompareBytes(actual, expected)
}

func passwordDigest(password string, salt []byte, iters int) []byte {
	if iters <= 0 {
		iters = passwordHashIters
	}
	state := make([]byte, 0, len(password)+len(salt)+16)
	state = append(state, []byte(password)...)
	state = append(state, salt...)

	sum := sha256.Sum256(state)
	buf := sum[:]
	for i := 1; i < iters; i++ {
		next := sha256.Sum256(buf)
		buf = next[:]
	}
	out := make([]byte, len(buf))
	copy(out, buf)
	return out
}

func subtleCompareBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func subtleCompare(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b))
}
