package auth
import "testing"

func TestHashAndMatch(t *testing.T) {
	key, err := GenerateDeviceKey()
	if err != nil {
		t.Fatalf("GenerateDeviceKey: %v", err)
	}
	hash := HashDeviceKey(key)
	if !KeysMatch(key, hash) {
		t.Fatal("correct key did not match its own hash")
	}
	if KeysMatch(key+"x", hash) {
		t.Fatal("modified key incorrectly matched")
	}
}

func TestPairingSingleUse(t *testing.T) {
	pm := NewPairingManager()
	token, _, err := pm.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !pm.Redeem(token) {
		t.Fatal("first redemption should succeed")
	}
	if pm.Redeem(token) {
		t.Fatal("second redemption of the same token should fail")
	}
}

func TestPairingUnknownToken(t *testing.T) {
	pm := NewPairingManager()
	if pm.Redeem("NOT-A-REAL-TOKEN") {
		t.Fatal("redeeming an unknown token should fail")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter()
	for i := 0; i < rateLimitMax; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("attempt %d should have been allowed", i)
		}
	}
	if rl.Allow("1.2.3.4") {
		t.Fatal("attempt beyond the limit should have been rejected")
	}
	if !rl.Allow("5.6.7.8") {
		t.Fatal("a different key should not share the same limit")
	}
}
