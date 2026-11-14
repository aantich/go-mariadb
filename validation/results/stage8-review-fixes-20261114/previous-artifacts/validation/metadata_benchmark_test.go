package validation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// greetingControl changes only the advertised server metadata-cache capability
// before the unmodified driver sees the greeting. The driver's normal capability
// negotiation then disables caching on both sides. Both benchmark alternatives
// use this wrapper, so accounting overhead is equivalent. No TLS is involved.
type greetingControl struct {
	net.Conn
	reader   io.Reader
	received *atomic.Uint64
}

func (c *greetingControl) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.received.Add(uint64(n))
	return n, err
}

func BenchmarkMariaDBMetadata(b *testing.B) {
	for _, cache := range []bool{false, true} {
		name := "uncached"
		if cache {
			name = "cached"
		}
		b.Run(name, func(b *testing.B) {
			cfg := driverConfig(b, "text")
			var received atomic.Uint64
			cfg.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
				conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
				if err != nil {
					return nil, err
				}
				// Reading the greeting is bounded by the normal fixture timeout.
				conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				var header [4]byte
				if _, err = io.ReadFull(conn, header[:]); err != nil {
					conn.Close()
					return nil, err
				}
				n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
				if n > 4096 {
					conn.Close()
					return nil, fmt.Errorf("unexpected greeting length %d", n)
				}
				body := make([]byte, n)
				if _, err = io.ReadFull(conn, body); err != nil {
					conn.Close()
					return nil, err
				}
				end := bytes.IndexByte(body[1:], 0) + 1
				highWord := end + 28
				if end < 1 || highWord+4 > len(body) {
					conn.Close()
					return nil, fmt.Errorf("malformed fixture greeting")
				}
				if body[highWord]&16 == 0 {
					conn.Close()
					return nil, fmt.Errorf("server did not advertise metadata caching")
				}
				if !cache {
					body[highWord] &^= 16
				} // MARIADB_CLIENT_CACHE_METADATA = 1 << 36.
				conn.SetReadDeadline(time.Time{})
				greeting := append(header[:], body...)
				return &greetingControl{conn, io.MultiReader(bytes.NewReader(greeting), conn), &received}, nil
			}
			db := openConfig(b, cfg)
			db.SetMaxOpenConns(1)
			columns := make([]string, 32)
			for i := range columns {
				columns[i] = fmt.Sprintf("42 AS c%d", i)
			}
			stmt, err := db.Prepare("SELECT " + strings.Join(columns, ","))
			if err != nil {
				b.Fatal(err)
			}
			defer stmt.Close()
			values := make([]int64, 32)
			dest := make([]any, 32)
			for i := range values {
				dest[i] = &values[i]
			}
			run := func() {
				if err := stmt.QueryRow().Scan(dest...); err != nil {
					b.Fatal(err)
				}
				for _, v := range values {
					if v != 42 {
						b.Fatal("cached metadata decoded wrong value")
					}
				}
			}
			run()
			run()
			before := received.Load()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				run()
			}
			b.StopTimer()
			b.ReportMetric(float64(received.Load()-before)/float64(b.N), "wire-B/op")
		})
	}
}
