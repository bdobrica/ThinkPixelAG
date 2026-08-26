// Package valkey provides disposable decision-cache and rate-limit
// acceleration. It never owns authorization or capacity truth.
package valkey

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

var ErrMiss = errors.New("valkey cache miss")

const maxValue = 64 << 10
const macSize = sha256.Size

type Cache struct {
	address, username, password string
	database                    int
	tlsConfig                   *tls.Config
	timeout                     time.Duration
	integrityKey                []byte
}

var _ ports.ThroughputAccelerator = (*Cache)(nil)

// Blocked reads only an integrity-protected exhausted-window marker. A miss or
// any error is bypassed by the caller in favor of PostgreSQL.
func (c *Cache) Blocked(ctx context.Context, key string) (bool, error) {
	value, err := c.Get(ctx, key)
	if errors.Is(err, ErrMiss) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(value) != "blocked" {
		return false, errors.New("invalid throughput marker")
	}
	return true, nil
}

func (c *Cache) MarkBlocked(ctx context.Context, key string, retryAt time.Time) error {
	ttl := time.Until(retryAt)
	if ttl <= 0 {
		return nil
	}
	return c.Set(ctx, key, []byte("blocked"), ttl)
}

func New(rawURL string, timeout time.Duration, integrityKey []byte) (*Cache, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "redis" && u.Scheme != "rediss") || u.RawQuery != "" || u.Fragment != "" || timeout <= 0 || len(integrityKey) < 32 {
		return nil, errors.New("invalid Valkey cache configuration")
	}
	db := 0
	if u.Path != "" && u.Path != "/" {
		db, err = strconv.Atoi(strings.TrimPrefix(u.Path, "/"))
		if err != nil || db < 0 || db > 15 {
			return nil, errors.New("invalid Valkey database")
		}
	}
	c := &Cache{address: u.Host, timeout: timeout, database: db, integrityKey: append([]byte(nil), integrityKey...)}
	if u.User != nil {
		c.username = u.User.Username()
		c.password, _ = u.User.Password()
	}
	if u.Scheme == "rediss" {
		c.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	}
	return c, nil
}
func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	reply, err := c.command(ctx, "GET", key)
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, ErrMiss
	}
	if len(reply) <= macSize || len(reply) > maxValue+macSize {
		return nil, errors.New("Valkey cache value exceeds bound")
	}
	provided, payload := reply[:macSize], reply[macSize:]
	if !hmac.Equal(provided, c.mac(key, payload)) {
		return nil, errors.New("Valkey cache integrity check failed")
	}
	return payload, nil
}
func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if len(value) == 0 || len(value) > maxValue || ttl <= 0 {
		return errors.New("invalid Valkey cache entry")
	}
	wrapped := append(c.mac(key, value), value...)
	_, err := c.command(ctx, "SET", key, string(wrapped), "PX", strconv.FormatInt(ttl.Milliseconds(), 10))
	return err
}
func (c *Cache) mac(key string, value []byte) []byte {
	mac := hmac.New(sha256.New, c.integrityKey)
	_, _ = mac.Write([]byte(key))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}
func (c *Cache) command(ctx context.Context, args ...string) ([]byte, error) {
	dialer := net.Dialer{Timeout: c.timeout}
	var conn net.Conn
	var err error
	if c.tlsConfig != nil {
		conn, err = tls.DialWithDialer(&dialer, "tcp", c.address, c.tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", c.address)
	}
	if err != nil {
		return nil, fmt.Errorf("connect Valkey: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	rw := bufio.NewReadWriter(bufio.NewReaderSize(conn, maxValue+1024), bufio.NewWriter(conn))
	if c.password != "" {
		auth := []string{"AUTH", c.password}
		if c.username != "" {
			auth = []string{"AUTH", c.username, c.password}
		}
		if err = writeCommand(rw, auth...); err != nil {
			return nil, err
		}
		if _, err = readReply(rw.Reader); err != nil {
			return nil, fmt.Errorf("authenticate Valkey: %w", err)
		}
	}
	if c.database != 0 {
		if err = writeCommand(rw, "SELECT", strconv.Itoa(c.database)); err != nil {
			return nil, err
		}
		if _, err = readReply(rw.Reader); err != nil {
			return nil, fmt.Errorf("select Valkey database: %w", err)
		}
	}
	if err = writeCommand(rw, args...); err != nil {
		return nil, err
	}
	return readReply(rw.Reader)
}
func writeCommand(rw *bufio.ReadWriter, args ...string) error {
	if _, err := fmt.Fprintf(rw, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if len(arg) > maxValue+macSize {
			return errors.New("Valkey command argument exceeds bound")
		}
		if _, err := fmt.Fprintf(rw, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return rw.Flush()
}
func readReply(r *bufio.Reader) ([]byte, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, fmt.Errorf("Valkey error: %s", line)
	case '$':
		n, err := strconv.Atoi(string(line))
		if err != nil || n < -1 || n > maxValue+macSize {
			return nil, errors.New("invalid Valkey bulk reply")
		}
		if n == -1 {
			return nil, nil
		}
		value := make([]byte, n+2)
		if _, err = io.ReadFull(r, value); err != nil {
			return nil, err
		}
		if string(value[n:]) != "\r\n" {
			return nil, errors.New("invalid Valkey bulk terminator")
		}
		return value[:n], nil
	default:
		return nil, errors.New("unsupported Valkey reply")
	}
}
func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	if err != nil || len(line) < 2 || line[len(line)-2] != '\r' || len(line) > 1024 {
		return nil, errors.New("invalid Valkey reply line")
	}
	return line[:len(line)-2], nil
}
