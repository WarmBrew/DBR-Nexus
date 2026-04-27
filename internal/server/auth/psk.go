package auth

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// PSKManager handles PSK-based agent authentication
type PSKManager struct {
	psk               []byte
	pending           map[string]*pendingChallenge // nonce -> challenge info
	mu                sync.Mutex
	challengeTTL      time.Duration
	encryptionEnabled bool
}

type pendingChallenge struct {
	nonce         string
	createdAt     time.Time
	serverPrivate *ecdh.PrivateKey
	serverNonce   []byte
}

// NewPSKManager creates a new PSK manager
func NewPSKManager(psk string) *PSKManager {
	return &PSKManager{
		psk:               []byte(psk),
		pending:           make(map[string]*pendingChallenge),
		challengeTTL:      30 * time.Second,
		encryptionEnabled: true,
	}
}

// SetEncryptionEnabled controls whether key exchange is included in challenges
func (m *PSKManager) SetEncryptionEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.encryptionEnabled = enabled
}

// ChallengeResult contains the challenge and optional key exchange data
type ChallengeResult struct {
	Nonce        string
	ServerPubKey string // base64 encoded X25519 public key (empty if encryption disabled)
	ServerNonce  string // base64 encoded 16-byte nonce (empty if encryption disabled)
}

// GenerateChallenge generates a random nonce for PSK challenge
func (m *PSKManager) GenerateChallenge() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	nonceBytes := make([]byte, 32)
	rand.Read(nonceBytes)
	nonce := hex.EncodeToString(nonceBytes)

	// Clean up expired challenges
	m.cleanup()

	m.pending[nonce] = &pendingChallenge{
		nonce:     nonce,
		createdAt: time.Now(),
	}

	return nonce
}

// GenerateChallengeWithKeyExchange generates a challenge with X25519 key exchange data
func (m *PSKManager) GenerateChallengeWithKeyExchange() (*ChallengeResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	// Generate X25519 ephemeral key pair
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate X25519 key: %w", err)
	}

	// Generate server nonce for key derivation
	serverNonce := make([]byte, 16)
	if _, err := rand.Read(serverNonce); err != nil {
		return nil, fmt.Errorf("generate server nonce: %w", err)
	}

	// Clean up expired challenges
	m.cleanup()

	m.pending[nonce] = &pendingChallenge{
		nonce:         nonce,
		createdAt:     time.Now(),
		serverPrivate: privateKey,
		serverNonce:   serverNonce,
	}

	return &ChallengeResult{
		Nonce:        nonce,
		ServerPubKey: base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()),
		ServerNonce:  base64.StdEncoding.EncodeToString(serverNonce),
	}, nil
}

// VerifyHMAC verifies the HMAC response from an agent
func (m *PSKManager) VerifyHMAC(nonce, hmacStr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	challenge, exists := m.pending[nonce]
	if !exists {
		return fmt.Errorf("unknown or expired challenge")
	}

	// Check challenge hasn't expired
	if time.Since(challenge.createdAt) > m.challengeTTL {
		delete(m.pending, nonce)
		return fmt.Errorf("challenge expired")
	}

	// Compute expected HMAC
	mac := hmac.New(sha256.New, m.psk)
	mac.Write([]byte(nonce))
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	// Remove used challenge — but keep serverPrivate for key derivation
	// We do NOT delete here; ComputeSharedSecret will clean up

	// Constant-time comparison
	if !hmac.Equal([]byte(hmacStr), []byte(expectedMAC)) {
		delete(m.pending, nonce)
		return fmt.Errorf("invalid HMAC")
	}

	return nil
}

// ComputeSharedSecret computes the ECDH shared secret using the stored server private key
// and the agent's public key. Returns the shared secret, server nonce, and an auth tag.
func (m *PSKManager) ComputeSharedSecret(nonce string, agentPubKeyB64 string) (shared []byte, serverNonce []byte, authTag []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	challenge, exists := m.pending[nonce]
	if !exists {
		return nil, nil, nil, fmt.Errorf("challenge not found")
	}

	// Clean up after use
	defer delete(m.pending, nonce)

	if challenge.serverPrivate == nil {
		return nil, nil, nil, fmt.Errorf("no key exchange data for this challenge")
	}

	agentPubKeyBytes, err := base64.StdEncoding.DecodeString(agentPubKeyB64)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("decode agent public key: %w", err)
	}

	agentPubKey, err := ecdh.X25519().NewPublicKey(agentPubKeyBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse agent public key: %w", err)
	}

	sharedSecret, err := challenge.serverPrivate.ECDH(agentPubKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("compute ECDH: %w", err)
	}

	// Compute auth tag: HMAC-SHA256(PSK, serverNonce || agentPubKey)
	tagMAC := hmac.New(sha256.New, m.psk)
	tagMAC.Write(challenge.serverNonce)
	tagMAC.Write(agentPubKeyBytes)
	authTag = tagMAC.Sum(nil)

	return sharedSecret, challenge.serverNonce, authTag, nil
}

// IsEncryptionEnabled returns whether encryption is enabled
func (m *PSKManager) IsEncryptionEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.encryptionEnabled
}

// cleanup removes expired challenges
func (m *PSKManager) cleanup() {
	now := time.Now()
	for nonce, challenge := range m.pending {
		if now.Sub(challenge.createdAt) > m.challengeTTL {
			delete(m.pending, nonce)
		}
	}
}
