package internal

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"math/big"

	"golang.org/x/crypto/pbkdf2"
)

const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
const hexCharset = "abcdef0123456789"

func RandomString(n int, set string) string {
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		b[i] = set[idx.Int64()]
	}
	return string(b)
}

func GenerateChallenge() string {
	return RandomString(64, hexCharset)
}

func GenerateSalt() string {
	return RandomString(64, charset)
}

func GenerateIterations() int {
	n, _ := rand.Int(rand.Reader, big.NewInt(4500))
	return int(n.Int64()) + 500
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// VerifyDeviceLoginPassword recomputes the device's loginPassword and compares.
// Algorithm (from HikAccessPushDemo DeviceRegistClass.checkLoginPassword):
//
//	tempPassword = HexToString(SHA256(username + salt + password)) + challenge
//	final = PBKDF2(tempPassword, salt, iterations, 64) as hex
//
// Returns ("", false) on mismatch, otherwise (digestType, true) where
// digestType is "sha256" or "sha1" (legacy compatibility).
func VerifyDeviceLoginPassword(username, password, salt, challenge string, iterations int, provided string) (string, bool) {
	temp := sha256Hex(username+salt+password) + challenge

	sha256Final := hex.EncodeToString(pbkdf2.Key([]byte(temp), []byte(salt), iterations, 64, sha256.New))
	if sha256Final == provided {
		return "sha256", true
	}
	sha1Final := hex.EncodeToString(pbkdf2.Key([]byte(temp), []byte(salt), iterations, 64, sha1.New))
	if sha1Final == provided {
		return "sha1", true
	}
	return "", false
}

// VerifyCustomAuth verifies the per-request My-Custom-Auth header.
// Algorithm (from HikAccessPushDemo ListenServerClass.checkCustomAuth):
//
//	final = HexToString(SHA256(HexToString(SHA256(username+salt+password)) + challenge))
func VerifyCustomAuth(username, password, salt, challenge string, provided string) bool {
	temp := sha256Hex(username+salt+password) + challenge
	expected := sha256Hex(temp)
	return expected == provided
}
