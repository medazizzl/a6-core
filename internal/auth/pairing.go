package auth
import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"sync"
	"time"
)

const pairingTokenTTL = 15 * time.Minute

type PairingManager struct {
	mu     sync.Mutex
	tokens map[string]time.Time
}

func NewPairingManager() *PairingManager {
	return &PairingManager{tokens: make(map[string]time.Time)}
}

func (p *PairingManager) Generate() (token string, expiresAt time.Time, err error) {
	b := make([]byte, 10)
	if _, err = rand.Read(b); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: generating pairing token: %w", err)
	}
	token = strings.ToUpper(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	expiresAt = time.Now().Add(pairingTokenTTL)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokens = make(map[string]time.Time)
	p.tokens[token] = expiresAt
	return token, expiresAt, nil
}

func (p *PairingManager) Redeem(token string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	expiry, ok := p.tokens[token]
	if !ok {
		return false
	}
	delete(p.tokens, token)
	return time.Now().Before(expiry)
}
