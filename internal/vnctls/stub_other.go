//go:build !linux || !arm

package vnctls

import (
	"errors"
	"net"
)

// Init is a no-op on non-Linux/ARM platforms
func Init() {}

// TLSConn is a stub for non-Linux/ARM platforms
type TLSConn struct{}

// UpgradeToTLS returns an error on non-Linux/ARM platforms
func UpgradeToTLS(conn net.Conn, useCert bool, certPEM, keyPEM string) (*TLSConn, error) {
	return nil, errors.New("OpenSSL TLS not available on this platform")
}

func (c *TLSConn) Read(b []byte) (int, error)  { return 0, errors.New("not implemented") }
func (c *TLSConn) Write(b []byte) (int, error) { return 0, errors.New("not implemented") }
func (c *TLSConn) Close() error                { return nil }
func (c *TLSConn) LocalAddr() net.Addr         { return nil }
func (c *TLSConn) RemoteAddr() net.Addr        { return nil }
func (c *TLSConn) GetCipherName() string       { return "" }
func (c *TLSConn) GetProtocolVersion() string  { return "" }

// IsHardwareCryptoEnabled returns false on non-Linux/ARM platforms
func IsHardwareCryptoEnabled() bool { return false }

// GetHardwareCryptoEngine returns "none" on non-Linux/ARM platforms
func GetHardwareCryptoEngine() string { return "none (platform not supported)" }
