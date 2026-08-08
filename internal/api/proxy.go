package api

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mybot/internal/cache"

	"go.uber.org/zap"
)

const (
	// proxyListURL adalah sumber daftar proxy (github raw).
	// Format tiap baris: "IP:PORT, TYPE, LATENCY" dengan TYPE = HTTP/SOCKS4/SOCKS5.
	proxyListURL = "https://raw.githubusercontent.com/daisymashiro/proxy-free/refs/heads/main/proxy_alive.txt"

	proxyFetchTimeout   = 20 * time.Second
	proxyHandshakeLimit = 20 * time.Second
)

// GetRandomProxy mengambil satu proxy acak dari cache.
func GetRandomProxy(ctx context.Context) (cache.Proxy, error) {
	list, err := GetProxyList(ctx)
	if err != nil {
		return cache.Proxy{}, err
	}
	if len(list) == 0 {
		return cache.Proxy{}, fmt.Errorf("daftar proxy kosong")
	}
	return list[rand.Intn(len(list))], nil
}

// GetProxyList mengembalikan daftar proxy. Ambil dari cache bila masih
// fresh (TTL 3 jam); kalau kedaluwarsa/kosong, fetch ulang. Kalau fetch
// ulang gagal tapi cache lama masih ada, pakai cache lama (failover).
func GetProxyList(ctx context.Context) ([]cache.Proxy, error) {
	list, fetchedAt := cache.GetProxyList()
	if len(list) > 0 && cache.IsProxyFresh(fetchedAt) {
		return list, nil
	}

	fresh, err := fetchAndCacheProxyList(ctx)
	if err == nil {
		return fresh, nil
	}
	if len(list) > 0 {
		zap.L().Warn("Fetch ulang proxy gagal, pakai cache lama",
			zap.Int("stale_total", len(list)),
			zap.Error(err),
		)
		return list, nil
	}
	return nil, err
}

// fetchAndCacheProxyList mengunduh daftar proxy dari proxyListURL lalu
// menyimpannya ke cache.
func fetchAndCacheProxyList(ctx context.Context) ([]cache.Proxy, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyListURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: proxyFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch daftar proxy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch daftar proxy: status %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	const maxProxies = 2000 // batas defensif: file sumber membeku/bengkak
	var proxies []cache.Proxy
	for sc.Scan() {
		if len(proxies) >= maxProxies {
			break
		}
		if p, ok := parseProxyLine(sc.Text()); ok {
			proxies = append(proxies, p)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("tidak ada proxy valid di daftar")
	}

	cache.SetProxyList(proxies)
	zap.L().Info("Proxy list dimuat", zap.Int("total", len(proxies)))
	return proxies, nil
}

// parseProxyLine mem-parse satu baris "IP:PORT, HTTP, 6ms" menjadi cache.Proxy.
// Baris invalid atau tipe tak dikenal dilewati.
func parseProxyLine(line string) (cache.Proxy, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return cache.Proxy{}, false
	}
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return cache.Proxy{}, false
	}

	addr := strings.TrimSpace(parts[0])
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return cache.Proxy{}, false
	}

	ptype := strings.ToUpper(strings.TrimSpace(parts[1]))
	switch ptype {
	case "HTTP", "SOCKS4", "SOCKS5":
	default:
		return cache.Proxy{}, false
	}

	return cache.Proxy{Addr: addr, Type: ptype}, true
}

// NewHTTPClientViaProxy membuat http.Client yang seluruh traffic-nya lewat
// proxy p. HTTP → CONNECT standar; SOCKS4/SOCKS5 → dialer handshake sendiri
// (httpcloak WithProxy tidak mendukung SOCKS4).
//
// InsecureSkipVerify DIPAKAI karena proxy gratis di proxy_alive.txt umumnya
// melakukan MITM TLS (pasang cert sendiri) — kalau diverifikasi, semua
// download lewat proxy gagal dengan x509 error. Aman untuk kasus ini:
// file yang diunduh adalah share publik; risiko terbesar (proxy menukar
// isi file) sebanding dengan risiko memakai proxy publik itu sendiri.
func NewHTTPClientViaProxy(p cache.Proxy, timeout time.Duration) (*http.Client, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}

	switch p.Type {
	case "HTTP":
		u, err := url.Parse("http://" + p.Addr)
		if err != nil {
			return nil, fmt.Errorf("proxy http invalid: %w", err)
		}
		tr.Proxy = http.ProxyURL(u)

	case "SOCKS4", "SOCKS5":
		d := &socksDialer{addr: p.Addr, typ: p.Type}
		tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return d.dial(ctx, network, address)
		}

	default:
		return nil, fmt.Errorf("tipe proxy tak didukung: %s", p.Type)
	}

	return &http.Client{Transport: tr, Timeout: timeout}, nil
}

// videoStreamUA adalah UA generik untuk unduh video lewat proxy (fallback).
const videoStreamUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

// GetVideoStreamWithProxyFallback mencoba stream normal (httpcloak) dulu;
// kalau gagal (blokir CDN/rate-limit/network), retry lewat proxy acak.
func GetVideoStreamWithProxyFallback(ctx context.Context, videoURL string) (io.ReadCloser, error) {
	stream, _, err := GetVideoStream(ctx, videoURL)
	if err == nil {
		return stream, nil
	}
	zap.L().Warn("Stream video normal gagal, coba lewat proxy", zap.Error(err))
	return OpenVideoStreamViaProxy(ctx, videoURL)
}

// OpenVideoStreamViaProxy mengunduh URL via hingga 3 proxy acak berbeda.
// Dipakai sebagai fallback saat jalur langsung gagal.
func OpenVideoStreamViaProxy(ctx context.Context, videoURL string) (io.ReadCloser, error) {
	used := map[string]bool{}
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		proxy, perr := GetRandomProxy(ctx)
		if perr != nil {
			return nil, perr
		}
		if used[proxy.Addr] {
			continue
		}
		used[proxy.Addr] = true

		client, cerr := NewHTTPClientViaProxy(proxy, 2*time.Hour)
		if cerr != nil {
			lastErr = cerr
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", videoStreamUA)
		req.Header.Set("Referer", "https://www.instagram.com/")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Accept-Encoding", "identity")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			zap.L().Warn("Percobaan stream via proxy gagal",
				zap.Int("attempt", attempt+1),
				zap.String("proxy", proxy.Addr),
				zap.Error(err))
			continue
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			lastErr = fmt.Errorf("status HTTP %d", resp.StatusCode)
			resp.Body.Close()
			continue
		}
		ct := resp.Header.Get("Content-Type")
		if ct != "" && (strings.HasPrefix(strings.ToLower(ct), "text/") || strings.Contains(strings.ToLower(ct), "html")) {
			lastErr = fmt.Errorf("unexpected content-type: %s", ct)
			resp.Body.Close()
			continue
		}

		zap.L().Info("Stream video berhasil via proxy",
			zap.String("proxy", proxy.Addr),
			zap.String("type", proxy.Type),
			zap.Int("attempt", attempt+1))
		return resp.Body, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("semua proxy sudah dicoba")
}

// ========== SOCKS dialer (stdlib, tanpa dependensi baru) ==========

type socksDialer struct {
	addr string
	typ  string
}

func (d *socksDialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	nc, err := (&net.Dialer{}).DialContext(ctx, network, d.addr)
	if err != nil {
		return nil, fmt.Errorf("koneksi proxy %s: %w", d.addr, err)
	}

	if d.typ == "SOCKS5" {
		if err := socks5Connect(nc, address); err != nil {
			nc.Close()
			return nil, err
		}
	} else {
		if err := socks4Connect(nc, address); err != nil {
			nc.Close()
			return nil, err
		}
	}
	return nc, nil
}

// socks5Connect menjalankan handshake SOCKS5 (tanpa auth) + CONNECT.
func socks5Connect(conn net.Conn, address string) error {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("socks5 target invalid: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("socks5 port invalid: %s", portStr)
	}

	_ = conn.SetDeadline(time.Now().Add(proxyHandshakeLimit))
	defer conn.SetDeadline(time.Time{})

	// Negosiasi metode: hanya no-auth (0x00).
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return fmt.Errorf("socks5 greeting: %w", err)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("socks5 greeting resp: %w", err)
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		return fmt.Errorf("socks5 metode auth ditolak: method=%d", buf[1])
	}

	req := append([]byte{0x05, 0x01, 0x00}, socks5AddressBytes(host, port)...)
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("socks5 connect: %w", err)
	}

	// Respons: VER, REP, RSV, ATYP, [addr], port
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("socks5 connect resp: %w", err)
	}
	if head[1] != 0x00 {
		return fmt.Errorf("socks5 connect ditolak: rep=%d", head[1])
	}
	rest, err := socks5ReplyLength(head[3])
	if err != nil {
		return err
	}
	if rest > 0 {
		if _, err := io.CopyN(io.Discard, conn, int64(rest)); err != nil {
			return fmt.Errorf("socks5 connect resp tail: %w", err)
		}
	}
	return nil
}

// socks5AddressBytes menyusun ATYP + address SOCKS5.
func socks5AddressBytes(host string, port int) []byte {
	var b []byte
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			b = append(b, 0x01)
			b = append(b, ip4...)
		} else {
			b = append(b, 0x04)
			b = append(b, ip.To16()...)
		}
	} else {
		b = append(b, 0x03, byte(len(host)))
		b = append(b, []byte(host)...)
	}
	var p [2]byte
	binary.BigEndian.PutUint16(p[:], uint16(port))
	return append(b, p[:]...)
}

// socks5ReplyLength menghitung sisa byte respons CONNECT berdasarkan ATYP.
func socks5ReplyLength(atyp byte) (int, error) {
	switch atyp {
	case 0x01:
		return 4 + 2, nil
	case 0x04:
		return 16 + 2, nil
	case 0x03:
		return -1, fmt.Errorf("socks5 server mengirim ATYP domain di respons") // jarang; aman stop
	default:
		return -1, fmt.Errorf("socks5 ATYP tak dikenal: %d", atyp)
	}
}

// socks4Connect menjalankan handshake SOCKS4(a) + CONNECT.
// Hostname ditangani lewat variant SOCKS4a (IP dummy 0.0.0.1 + nama domain).
func socks4Connect(conn net.Conn, address string) error {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("socks4 target invalid: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("socks4 port invalid: %s", portStr)
	}

	_ = conn.SetDeadline(time.Now().Add(proxyHandshakeLimit))
	defer conn.SetDeadline(time.Time{})

	req := []byte{0x04, 0x01}
	var p [2]byte
	binary.BigEndian.PutUint16(p[:], uint16(port))
	req = append(req, p[:]...)
	if ip := net.ParseIP(host).To4(); ip != nil {
		req = append(req, ip...)
		req = append(req, 0) // userid kosong
	} else {
		// SOCKS4a: IP 0.0.0.1 + userid kosong + domain.
		req = append(req, 0, 0, 0, 1, 0)
		req = append(req, []byte(host)...)
		req = append(req, 0)
	}

	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("socks4 connect: %w", err)
	}
	resp := make([]byte, 8)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("socks4 connect resp: %w", err)
	}
	if resp[1] != 0x5A {
		return fmt.Errorf("socks4 ditolak: cd=%d", resp[1])
	}
	return nil
}
