package ota

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// VerifySignatureReader verifies a detached GPG signature against what data
// yields up to its end, so a download is checked as it streams. The
// signature is binary. data is read only once the signature is known to be
// by the release key.
func (g *GPGVerifier) VerifySignatureReader(ctx context.Context, signature []byte, data io.Reader) error {
	keyring, err := g.getKeyring(ctx)
	if err != nil {
		return err
	}
	if _, err := openpgp.CheckDetachedSignature(keyring, data, bytes.NewReader(signature), nil); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}
