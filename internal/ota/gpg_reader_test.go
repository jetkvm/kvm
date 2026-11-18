package ota

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A stream that arrives in pieces verifies like the whole bytes; a changed
// byte or another key's signature fails.
func TestVerifySignatureReader(t *testing.T) {
	t.Parallel()
	release := generateEntityWithSigningSubkey(t)
	rogue := generateEntityWithSigningSubkey(t)
	v := newTestGPGVerifier()
	v.rootKeyFP = strings.ToUpper(hex.EncodeToString(release.PrimaryKey.Fingerprint[:]))
	armored := armorEntity(t, release)
	keyring, err := v.parseAndValidateKeyring(armored)
	require.NoError(t, err)
	v.updateMemoryCache(armored, keyring)

	data := bytes.Repeat([]byte("os services package "), 10000)
	sig := signWithEntity(t, release, data)

	verify := func(sig, data []byte) error {
		r, w := io.Pipe()
		go func() {
			for chunk := range 100 {
				w.Write(data[chunk*len(data)/100 : (chunk+1)*len(data)/100])
			}
			w.Close()
		}()
		err := v.VerifySignatureReader(context.Background(), sig, r)
		r.Close()
		return err
	}
	require.NoError(t, verify(sig, data))

	changed := append([]byte(nil), data...)
	changed[len(changed)/2] ^= 1
	require.Error(t, verify(sig, changed))
	require.Error(t, verify(signWithEntity(t, rogue, data), data))
}
