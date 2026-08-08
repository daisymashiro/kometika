package api

import (
	"bytes"
	"net"
	"testing"

	"mybot/internal/cache"
)

func TestParseProxyLine(t *testing.T) {
	cases := []struct {
		line string
		ok   bool
		want cache.Proxy
	}{
		{"138.197.208.93:8080, HTTP, 6ms", true, cache.Proxy{Addr: "138.197.208.93:8080", Type: "HTTP"}},
		{"43.153.82.179:8888, SOCKS4, 21ms", true, cache.Proxy{Addr: "43.153.82.179:8888", Type: "SOCKS4"}},
		{"146.235.231.249:1080, SOCKS5, 25ms", true, cache.Proxy{Addr: "146.235.231.249:1080", Type: "SOCKS5"}},
		{"", false, cache.Proxy{}},
		{"no-port, HTTP, 1ms", false, cache.Proxy{}},
		{"1.2.3.4:80, FTP, 1ms", false, cache.Proxy{}}, // tipe tak dikenal
		{"1.2.3.4:80", false, cache.Proxy{}},           // kurang kolom type
	}
	for _, c := range cases {
		got, ok := parseProxyLine(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("parseProxyLine(%q) = (%+v, %v), want (%+v, %v)", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestSocks5AddressBytes(t *testing.T) {
	if got := socks5AddressBytes("1.2.3.4", 8080); !bytes.Equal(got, []byte{0x01, 1, 2, 3, 4, 0x1f, 0x90}) {
		t.Errorf("ipv4: got %x", got)
	}
	if got := socks5AddressBytes("cdn.example.com", 443); !bytes.Equal(got, []byte{0x03, 15, 'c', 'd', 'n', '.', 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm', 0x01, 0xbb}) {
		t.Errorf("domain: got %x", got)
	}
}

// socks5TargetServer mensimulasikan server SOCKS5 minimal (no-auth + CONNECT ok).
func socks5TargetServer(t *testing.T, conn net.Conn) {
	t.Helper()
	greet := make([]byte, 3)
	if _, err := readFull(conn, greet); err != nil {
		t.Fatalf("greeting: %v", err)
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}
	req := make([]byte, 4)
	if _, err := readFull(conn, req); err != nil {
		t.Fatalf("connect req: %v", err)
	}
	// balas sukses dengan target dummy IPv4
	_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 9, 9, 9, 9, 0x1f, 0x90})
}

func TestSocks5Connect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		socks5TargetServer(t, conn)
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if err := socks5Connect(client, "1.2.3.4:443"); err != nil {
		t.Fatalf("socks5Connect: %v", err)
	}
}

func TestSocks4Connect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		req := make([]byte, 9)
		if _, err := readFull(conn, req); err != nil {
			t.Errorf("socks4 req: %v", err)
			return
		}
		_, _ = conn.Write([]byte{0x00, 0x5A, 0, 0, 0, 0, 0, 0})
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if err := socks4Connect(client, "1.2.3.4:443"); err != nil {
		t.Fatalf("socks4Connect: %v", err)
	}
}

func readFull(c net.Conn, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := c.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}