package api

import (
	"crypto/md5"
	"encoding/hex"
)

// shortID menghasilkan ID pendek dari URL (untuk fallback jika API tidak memberikan ID)
func ShortID(url string) string {
	hash := md5.Sum([]byte(url))
	return hex.EncodeToString(hash[:])[:16]
}
