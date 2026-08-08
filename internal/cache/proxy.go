package cache

import (
	"sync"
	"time"
)

// Proxy adalah satu entri proxy dari daftar proxy_alive.txt.
// Type: HTTP / SOCKS4 / SOCKS5.
type Proxy struct {
	Addr string
	Type string
}

// ProxyCacheTTL menentukan umur cache daftar proxy.
// Sumber (proxy_alive.txt) hanya dicek ~2x per 24 jam oleh penyedia,
// jadi TTL 3 jam cukup segar dan hemat request.
const ProxyCacheTTL = 3 * time.Hour

var (
	proxyMu       sync.Mutex
	proxyList     []Proxy
	proxyFetched  time.Time
)

// SetProxyList menyimpan daftar proxy beserta waktu fetch.
func SetProxyList(list []Proxy) {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	proxyList = append([]Proxy(nil), list...)
	proxyFetched = time.Now()
}

// GetProxyList mengembalikan salinan daftar proxy + waktu fetch terakhir.
// Slice kosong menandakan cache belum terisi.
func GetProxyList() ([]Proxy, time.Time) {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	return append([]Proxy(nil), proxyList...), proxyFetched
}

// IsProxyFresh true bila cache terisi dan belum melewati ProxyCacheTTL.
func IsProxyFresh(fetchedAt time.Time) bool {
	return !fetchedAt.IsZero() && time.Since(fetchedAt) < ProxyCacheTTL
}