package openai

import (
	"crypto/rand"
	"encoding/hex"
)

func randomID() string {
	value := make([]byte, 12)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}
