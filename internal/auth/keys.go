package auth
import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

func GenerateDeviceKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generating device key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashDeviceKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func KeysMatch(candidateKey, storedHash string) bool {
	candidateHash := HashDeviceKey(candidateKey)
	return subtle.ConstantTimeCompare([]byte(candidateHash), []byte(storedHash)) == 1
}
