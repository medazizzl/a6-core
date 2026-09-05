// Package selfpair implements the TV shell's self-pairing flow
// described in the Stage 17 blueprint §2: on first run, the shell
// pairs itself as a device using A6 Core's existing pairing flow
// (no bypass, no special-case endpoint) and stores its device key
// locally. On every later run it reuses that stored key instead of
// pairing again.
//
// This package does not build or run the shell itself — it is the
// small, self-contained piece the eventual shell binary will import
// and call once at startup, before it does anything else.
package selfpair

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	keyDirName  = ".config/a6-shell"
	keyFileName = "device_key"
	deviceName  = "TV Shell"
)

// ErrStoredKeyRejected means a device key file exists locally but
// A6 Core no longer accepts it (revoked, or its state was rebuilt).
// Callers must treat this as fatal and stop — silently re-pairing
// here would let a revoked device quietly regain access, which the
// blueprint explicitly forbids.
var ErrStoredKeyRejected = errors.New("selfpair: stored device key was rejected by A6 Core")

// KeyFilePath returns the path a device key is read from and
// written to: ~/.config/a6-shell/device_key.
func KeyFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("selfpair: could not determine home directory: %w", err)
	}
	return filepath.Join(home, keyDirName, keyFileName), nil
}

// EnsureDeviceKey returns a valid device key for talking to A6 Core
// at baseURL (e.g. "http://127.0.0.1:7887"), pairing for the first
// time if no key is stored yet.
//
//   - If a key file exists, it is validated against A6 Core before
//     being trusted. A rejected stored key returns
//     ErrStoredKeyRejected and nothing is re-paired automatically.
//   - If no key file exists, this pairs for the first time: generate
//     a pairing token (the same localhost-only endpoint a phone's QR
//     code would otherwise redeem), redeem it immediately, and save
//     the result.
func EnsureDeviceKey(baseURL string) (string, error) {
	path, err := KeyFilePath()
	if err != nil {
		return "", err
	}

	existing, err := os.ReadFile(path)
	if err == nil {
		key := string(bytes.TrimSpace(existing))
		if key == "" {
			return "", fmt.Errorf("selfpair: key file %s exists but is empty", path)
		}
		if !validateKey(baseURL, key) {
			return "", fmt.Errorf("%w (path: %s)", ErrStoredKeyRejected, path)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("selfpair: reading key file %s: %w", path, err)
	}

	key, err := pairFirstTime(baseURL)
	if err != nil {
		return "", err
	}
	if err := saveKey(path, key); err != nil {
		return "", err
	}
	return key, nil
}

// validateKey asks A6 Core itself whether this key is still good, by
// calling an authenticated endpoint and checking for success. This
// reuses RequireDevice exactly as a phone's own requests would — no
// special-case validation path just for the shell.
func validateKey(baseURL, key string) bool {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/info", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

type pairGenerateResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type pairRequest struct {
	Token      string `json:"token"`
	DeviceName string `json:"device_name"`
}

type pairResponse struct {
	DeviceID   string `json:"device_id"`
	DeviceKey  string `json:"device_key"`
	CoreName   string `json:"core_name"`
	APIVersion string `json:"api_version"`
}

// pairFirstTime performs the two-step pairing flow a phone's QR-code
// scan would otherwise drive by hand: generate a token, then
// immediately redeem it. Since this only ever runs on A6 Core's own
// machine, /v1/pair/generate's RequireLocalhost check is satisfied
// naturally — no bypass, no separate endpoint.
func pairFirstTime(baseURL string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	genResp, err := client.Post(baseURL+"/v1/pair/generate", "application/json", nil)
	if err != nil {
		return "", fmt.Errorf("selfpair: requesting pairing token: %w", err)
	}
	defer genResp.Body.Close()
	if genResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("selfpair: pairing token request failed with status %d", genResp.StatusCode)
	}
	var gen pairGenerateResponse
	if err := json.NewDecoder(genResp.Body).Decode(&gen); err != nil {
		return "", fmt.Errorf("selfpair: decoding pairing token response: %w", err)
	}

	body, err := json.Marshal(pairRequest{Token: gen.Token, DeviceName: deviceName})
	if err != nil {
		return "", fmt.Errorf("selfpair: encoding pairing request: %w", err)
	}

	pairResp, err := client.Post(baseURL+"/v1/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("selfpair: redeeming pairing token: %w", err)
	}
	defer pairResp.Body.Close()
	if pairResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("selfpair: pairing redeem failed with status %d", pairResp.StatusCode)
	}
	var pr pairResponse
	if err := json.NewDecoder(pairResp.Body).Decode(&pr); err != nil {
		return "", fmt.Errorf("selfpair: decoding pairing redeem response: %w", err)
	}
	if pr.DeviceKey == "" {
		return "", errors.New("selfpair: pairing succeeded but no device key was returned")
	}
	return pr.DeviceKey, nil
}

// saveKey writes key to path with 0600 permissions, creating the
// containing directory (0700) first if needed. The directory and
// file modes matter here: this file is exactly as sensitive as a
// phone's device key, and the blueprint requires it never be
// world-readable.
func saveKey(path, key string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("selfpair: creating %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return fmt.Errorf("selfpair: writing %s: %w", path, err)
	}
	return nil
}
