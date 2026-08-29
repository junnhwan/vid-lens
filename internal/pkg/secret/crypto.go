package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

type Codec struct {
	aead cipher.AEAD
}

func NewCodecFromPassphrase(passphrase string) (*Codec, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("api key secret is required")
	}
	sum := sha256.Sum256([]byte(passphrase))
	return newCodecFromKey(sum[:])
}

func NewCodec(secret string) (*Codec, error) {
	key := []byte(secret)
	switch len(key) {
	case 16, 24, 32:
	default:
		return nil, fmt.Errorf("api key secret length must be 16, 24, or 32 bytes")
	}
	return newCodecFromKey(key)
}

func newCodecFromKey(key []byte) (*Codec, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Codec{aead: aead}, nil
}

func (c *Codec) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	payload := make([]byte, 0, len(nonce)+len(sealed))
	payload = append(payload, nonce...)
	payload = append(payload, sealed...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func (c *Codec) Decrypt(ciphertext string) (string, error) {
	payload, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	nonceSize := c.aead.NonceSize()
	if len(payload) <= nonceSize {
		return "", fmt.Errorf("ciphertext is too short")
	}

	nonce := payload[:nonceSize]
	sealed := payload[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func MaskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:3] + "****" + key[len(key)-4:]
}
