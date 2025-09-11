package utils

import (
	"fmt"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"
)

// ValidSSHKeyTypes is a list of valid SSH key types
//
// Please make sure that all the types in this list are supported by dropbear
// https://github.com/mkj/dropbear/blob/003c5fcaabc114430d5d14142e95ffdbbd2d19b6/src/signkey.c#L37
//
// ssh-dss is not allowed here as it's insecure
var ValidSSHKeyTypes = []string{
	ssh.KeyAlgoRSA,
	ssh.KeyAlgoED25519,
	ssh.KeyAlgoECDSA256,
	ssh.KeyAlgoECDSA384,
	ssh.KeyAlgoECDSA521,
}

// ValidateSSHKey validates authorized_keys file content
func ValidateSSHKey(sshKey string) error {
	// validate SSH key
	var (
		hasValidPublicKey = false
		lastError         = fmt.Errorf("no valid SSH key found")
	)
	for _, key := range strings.Split(sshKey, "\n") {
		key = strings.TrimSpace(key)

		// skip empty lines and comments
		if key == "" || strings.HasPrefix(key, "#") {
			continue
		}

		parsedPublicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key))
		if err != nil {
			lastError = err
			continue
		}

		if parsedPublicKey == nil {
			continue
		}

		parsedType := parsedPublicKey.Type()
		textType := strings.Fields(key)[0]

		if parsedType != textType {
			lastError = fmt.Errorf("parsed SSH key type %s does not match type in text %s", parsedType, textType)
			continue
		}

		if !slices.Contains(ValidSSHKeyTypes, parsedType) {
			lastError = fmt.Errorf("invalid SSH key type: %s", parsedType)
			continue
		}

		hasValidPublicKey = true
	}

	if !hasValidPublicKey {
		return lastError
	}

	return nil
}
