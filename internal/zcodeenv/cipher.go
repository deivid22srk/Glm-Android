package zcodeenv

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/user"
	"runtime"
	"strings"
)

const encryptedPrefix = "enc:v1:"

type Cipher struct {
	key [32]byte
}

func NewCipher(home string) Cipher {
	return Cipher{key: sha256.Sum256([]byte(credentialSecret(home)))}
}

func (c Cipher) Encrypt(value string) (string, error) {
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, []byte(value), nil)
	tagSize := gcm.Overhead()
	ciphertext := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]
	return encryptedPrefix + rawURL(iv) + "." + rawURL(tag) + "." + rawURL(ciphertext), nil
}

func (c Cipher) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return value, nil
	}
	parts := strings.Split(strings.TrimPrefix(value, encryptedPrefix), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid encrypted credential")
	}
	iv, err := rawURLDecode(parts[0])
	if err != nil {
		return "", err
	}
	tag, err := rawURLDecode(parts[1])
	if err != nil {
		return "", err
	}
	ciphertext, err := rawURLDecode(parts[2])
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, iv, append(ciphertext, tag...), nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func credentialSecret(home string) string {
	if value := os.Getenv("ZCODE_CREDENTIAL_SECRET"); value != "" {
		return value
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	username := os.Getenv("USERNAME")
	if username == "" {
		if current, err := user.Current(); err == nil {
			username = current.Username
			if index := strings.LastIndexAny(username, `\`); index >= 0 {
				username = username[index+1:]
			}
		}
	}
	return "zcode-credential-fallback:" + nodePlatform() + ":" + home + ":" + username
}

func nodePlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	default:
		return runtime.GOOS
	}
}

func rawURL(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}

func rawURLDecode(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(value)
}
