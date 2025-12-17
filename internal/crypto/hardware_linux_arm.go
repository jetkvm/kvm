//go:build linux && arm

package crypto

import (
	"crypto/cipher"
	"errors"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// Cryptodev ioctl commands
// CIOCAUTHCRYPT = _IOWR('c', 109, struct crypt_auth_op)
// On 32-bit ARM with 44-byte struct crypt_auth_op: 0xc02c636d
const (
	ciocgsession  = 0xc01c6366
	ciocfsession  = 0x40046367
	ciocauthcrypt = 0xc02c636d // _IOWR('c', 109, 44) - correct size for 32-bit ARM
)

// Cipher types
const (
	cryptoAESGCM   = 50  // Standard CRYPTO_AES_GCM
	cryptoRKAESGCM = 177 // Rockchip-specific (may not work with cryptodev)
)

// Operation types
const (
	copEncrypt = 0
	copDecrypt = 1
)

var (
	cryptodev     *os.File
	cryptodevOnce sync.Once
	cryptodevErr  error
)

func getCryptodev() (*os.File, error) {
	cryptodevOnce.Do(func() {
		cryptodev, cryptodevErr = os.OpenFile("/dev/crypto", os.O_RDWR, 0)
	})
	return cryptodev, cryptodevErr
}

func ioctl(fd uintptr, op uintptr, data unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, op, uintptr(data))
	if errno != 0 {
		return os.NewSyscallError("ioctl", errno)
	}
	return nil
}

// sessionOp is the structure for creating a crypto session
type sessionOp struct {
	Cipher    uint32
	Mac       uint32
	Keylen    uint32
	Key       unsafe.Pointer
	Mackeylen uint32
	Mackey    unsafe.Pointer
	Id        uint32
}

// cryptAuthOp is the structure for authenticated encryption operations
// Must match struct crypt_auth_op in uapi/linux/cryptodev.h exactly
type cryptAuthOp struct {
	Ses     uint32         // Session identifier (32-bit)
	Op      uint16         // COP_ENCRYPT or COP_DECRYPT
	Flags   uint16         // Operation flags (COP_FLAG_AEAD_RK_TYPE for Rockchip)
	Len     uint32         // Length of src/dst data
	AuthLen uint32         // Length of auth data
	AuthSrc unsafe.Pointer // Additional authenticated data
	Src     unsafe.Pointer // Input data
	Dst     unsafe.Pointer // Output data
	Tag     unsafe.Pointer // Authentication tag
	TagLen  uint32         // Tag length
	Iv      unsafe.Pointer // IV/nonce
	IvLen   uint32         // IV length
}

// Rockchip-specific AEAD flag
const copFlagAEADRKType = 1 << 11

// hardwareAESGCM implements AEAD using Rockchip hardware acceleration
type hardwareAESGCM struct {
	fd        uintptr
	sessionOp *sessionOp
	nonceSize int
	tagSize   int
	mu        sync.Mutex
}

// Compile-time check
var _ cipher.AEAD = (*hardwareAESGCM)(nil)

func newHardwareAESGCM(key []byte) (AEAD, error) {
	handle, err := getCryptodev()
	if err != nil {
		return nil, err
	}
	fd := handle.Fd()

	// Make a copy of the key to ensure it doesn't get GC'd
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	sess := &sessionOp{
		Cipher: cryptoRKAESGCM, // Use Rockchip CRYPTO_RK_AES_GCM
		Keylen: uint32(len(keyCopy)),
		Key:    unsafe.Pointer(&keyCopy[0]),
	}

	err = ioctl(fd, ciocgsession, unsafe.Pointer(sess))
	if err != nil {
		return nil, err
	}

	return &hardwareAESGCM{
		fd:        fd,
		sessionOp: sess,
		nonceSize: 12,
		tagSize:   16,
	}, nil
}

func (g *hardwareAESGCM) NonceSize() int {
	return g.nonceSize
}

func (g *hardwareAESGCM) Overhead() int {
	return g.tagSize
}

func (g *hardwareAESGCM) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.sessionOp != nil {
		err := ioctl(g.fd, ciocfsession, unsafe.Pointer(g.sessionOp))
		g.sessionOp = nil
		return err
	}
	return nil
}

func (g *hardwareAESGCM) IsHardwareAccelerated() bool {
	return true
}

func (g *hardwareAESGCM) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
	if len(nonce) != g.nonceSize {
		panic("crypto: incorrect nonce length")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Allocate output: ciphertext + tag
	ret, out := sliceForAppend(dst, len(plaintext)+g.tagSize)
	ciphertext := out[:len(plaintext)]
	tag := out[len(plaintext):]

	// Make copies to ensure memory safety
	nonceCopy := make([]byte, len(nonce))
	copy(nonceCopy, nonce)

	authOp := &cryptAuthOp{
		Ses:    g.sessionOp.Id,
		Op:     copEncrypt,
		Flags:  copFlagAEADRKType, // Use Rockchip AEAD mode
		Len:    uint32(len(plaintext)),
		TagLen: uint32(g.tagSize),
		IvLen:  uint32(len(nonceCopy)),
		Iv:     unsafe.Pointer(&nonceCopy[0]),
		Tag:    unsafe.Pointer(&tag[0]),
	}

	if len(additionalData) > 0 {
		authOp.AuthSrc = unsafe.Pointer(&additionalData[0])
		authOp.AuthLen = uint32(len(additionalData))
	}

	if len(plaintext) > 0 {
		authOp.Src = unsafe.Pointer(&plaintext[0])
		authOp.Dst = unsafe.Pointer(&ciphertext[0])
	}

	err := ioctl(g.fd, ciocauthcrypt, unsafe.Pointer(authOp))
	if err != nil {
		panic("crypto: hardware encryption failed: " + err.Error())
	}

	return ret
}

func (g *hardwareAESGCM) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	if len(nonce) != g.nonceSize {
		return nil, errors.New("crypto: incorrect nonce length")
	}
	if len(ciphertext) < g.tagSize {
		return nil, errors.New("crypto: ciphertext too short")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Split ciphertext and tag
	tagStart := len(ciphertext) - g.tagSize
	actualCiphertext := ciphertext[:tagStart]
	tag := ciphertext[tagStart:]

	ret, out := sliceForAppend(dst, len(actualCiphertext))

	// Make copies to ensure memory safety
	nonceCopy := make([]byte, len(nonce))
	copy(nonceCopy, nonce)
	tagCopy := make([]byte, len(tag))
	copy(tagCopy, tag)

	authOp := &cryptAuthOp{
		Ses:    g.sessionOp.Id,
		Op:     copDecrypt,
		Flags:  copFlagAEADRKType, // Use Rockchip AEAD mode
		Len:    uint32(len(actualCiphertext)),
		TagLen: uint32(g.tagSize),
		IvLen:  uint32(len(nonceCopy)),
		Iv:     unsafe.Pointer(&nonceCopy[0]),
		Tag:    unsafe.Pointer(&tagCopy[0]),
	}

	if len(additionalData) > 0 {
		authOp.AuthSrc = unsafe.Pointer(&additionalData[0])
		authOp.AuthLen = uint32(len(additionalData))
	}

	if len(actualCiphertext) > 0 {
		authOp.Src = unsafe.Pointer(&actualCiphertext[0])
		authOp.Dst = unsafe.Pointer(&out[0])
	}

	err := ioctl(g.fd, ciocauthcrypt, unsafe.Pointer(authOp))
	if err != nil {
		return nil, errors.New("crypto: authentication failed")
	}

	return ret, nil
}

func sliceForAppend(in []byte, n int) (head, tail []byte) {
	if total := len(in) + n; cap(in) >= total {
		head = in[:total]
	} else {
		head = make([]byte, total)
		copy(head, in)
	}
	tail = head[len(in):]
	return
}
