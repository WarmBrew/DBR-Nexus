package crypto

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/hkdf"
)

// KeyPair holds an X25519 key pair for key exchange
type KeyPair struct {
	Private *ecdh.PrivateKey
	Public  *ecdh.PublicKey
}

// GenerateX25519KeyPair generates a new X25519 key pair
func GenerateX25519KeyPair() (*KeyPair, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate X25519 key: %w", err)
	}
	return &KeyPair{
		Private: private,
		Public:  private.PublicKey(),
	}, nil
}

// ComputeSharedSecret computes the ECDH shared secret using our private key and peer's public key
func ComputeSharedSecret(ourPrivate *ecdh.PrivateKey, peerPublicBytes []byte) ([]byte, error) {
	peerPublic, err := ecdh.X25519().NewPublicKey(peerPublicBytes)
	if err != nil {
		return nil, fmt.Errorf("parse peer public key: %w", err)
	}
	shared, err := ourPrivate.ECDH(peerPublic)
	if err != nil {
		return nil, fmt.Errorf("compute ECDH shared secret: %w", err)
	}
	return shared, nil
}

// DerivedKeys holds all keys derived from the key exchange
type DerivedKeys struct {
	EncryptKey   [32]byte // AES-256-GCM encryption key
	HeaderMACKey [32]byte // HMAC key for routing header authentication
	NonceBase    [12]byte // Base for nonce construction
	CodeMapSeed  [16]byte // Seed for channel/action code mapping
}

// KeyDerivationContext contains the context for HKDF key derivation
type KeyDerivationContext struct {
	SharedSecret []byte
	SessionLabel string // "dbr-nexus-agent-v1" or "dbr-nexus-browser-v1"
	ServerNonce  []byte // 16 bytes
	ClientNonce  []byte // 16 bytes
	AuthTag      []byte // HMAC-SHA256(PSK, serverNonce||clientNonce) or SHA256(JWT)
}

// DeriveKeys derives all session keys from the ECDH shared secret
func DeriveKeys(ctx *KeyDerivationContext) (*DerivedKeys, error) {
	// Construct HKDF info: label || serverNonce || clientNonce || authTag
	info := make([]byte, 0, len(ctx.SessionLabel)+16+16+32)
	info = append(info, []byte(ctx.SessionLabel)...)
	info = append(info, ctx.ServerNonce...)
	info = append(info, ctx.ClientNonce...)
	info = append(info, ctx.AuthTag...)

	// Derive 92 bytes: 32 (encrypt) + 32 (mac) + 12 (nonce) + 16 (code map seed)
	r := hkdf.New(sha256.New, ctx.SharedSecret, nil, info)
	derived := make([]byte, 92)
	if _, err := r.Read(derived); err != nil {
		return nil, fmt.Errorf("HKDF derive: %w", err)
	}

	var keys DerivedKeys
	copy(keys.EncryptKey[:], derived[0:32])
	copy(keys.HeaderMACKey[:], derived[32:64])
	copy(keys.NonceBase[:], derived[64:76])
	copy(keys.CodeMapSeed[:], derived[76:92])

	return &keys, nil
}

// GenerateNonce generates a 16-byte random nonce
func GenerateNonce() ([]byte, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return nonce, nil
}
