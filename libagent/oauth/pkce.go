package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// pkce holds a PKCE verifier/challenge pair.
type pkce struct {
	Verifier  string
	Challenge string
}

// generatePKCE returns a fresh PKCE verifier and its S256 challenge.
func generatePKCE() (pkce, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return pkce{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)

	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	return pkce{Verifier: verifier, Challenge: challenge}, nil
}
