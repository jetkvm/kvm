package utils

import (
	"strings"
	"testing"
)

func TestValidateSSHKey(t *testing.T) {
	tests := []struct {
		name        string
		sshKey      string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid RSA key",
			sshKey:      "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDiYUb9Fy2vlPfO+HwubnshimpVrWPoePyvyN+jPC5gWqZSycjMy6Is2vFVn7oQc72bkY0wZalspT5wUOwKtltSoLpL7vcqGL9zHVw4yjYXtPGIRd3zLpU9wdngevnepPQWTX3LvZTZfmOsrGoMDKIG+Lbmiq/STMuWYecIqMp7tUKRGS8vfAmpu6MsrN9/4UTcdWWXYWJQQn+2nCyMz28jYlWRsKtqFK6owrdZWt8WQnPN+9Upcf2ByQje+0NLnpNrnh+yd2ocuVW9wQYKAZXy7IaTfEJwd5m34sLwkqlZTaBBcmWJU+3RfpYXE763cf3rUoPIGQ8eUEBJ8IdM4vhp test@example.com",
			expectError: false,
		},
		{
			name:        "valid ED25519 key",
			sshKey:      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBSbM8wuD5ab0nHsXaYOqaD3GLLUwmDzSk79Xi/N+H2j test@example.com",
			expectError: false,
		},
		{
			name:        "valid ECDSA key",
			sshKey:      "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBAlTkxIo4mXBR+gEX0Q74BpYX4bFFHoX+8Uz7tsob8HvsnMvsEE+BW9h9XrbWX4/4ppL/o6sHbvsqNr9HcyKfdc= test@example.com",
			expectError: false,
		},
		{
			name:        "valid SK-backed ED25519 key",
			sshKey:      "sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lZDI1NTE5QG9wZW5zc2guY29tAAAAIHHSRVC3qISk/mOorf24au6esimA9Uu1/BkEnVKJ+4bFAAAABHNzaDo= test@example.com",
			expectError: false,
		},
		{
			name:        "valid SK-backed ECDSA key",
			sshKey:      "sk-ecdsa-sha2-nistp256@openssh.com AAAAInNrLWVjZHNhLXNoYTItbmlzdHAyNTZAb3BlbnNzaC5jb20AAAAIbmlzdHAyNTYAAABBBL/CFBZksvs+gJODMB9StxnkY6xRKH73npOzJBVb0UEGCPTAhDrvzW1PE5X5GDYXmZw1s7c/nS+GH0LF0OFCpwAAAAAEc3NoOg== test@example.com",
			expectError: false,
		},
		{
			name:        "multiple valid keys",
			sshKey:      "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDiYUb9Fy2vlPfO+HwubnshimpVrWPoePyvyN+jPC5gWqZSycjMy6Is2vFVn7oQc72bkY0wZalspT5wUOwKtltSoLpL7vcqGL9zHVw4yjYXtPGIRd3zLpU9wdngevnepPQWTX3LvZTZfmOsrGoMDKIG+Lbmiq/STMuWYecIqMp7tUKRGS8vfAmpu6MsrN9/4UTcdWWXYWJQQn+2nCyMz28jYlWRsKtqFK6owrdZWt8WQnPN+9Upcf2ByQje+0NLnpNrnh+yd2ocuVW9wQYKAZXy7IaTfEJwd5m34sLwkqlZTaBBcmWJU+3RfpYXE763cf3rUoPIGQ8eUEBJ8IdM4vhp test@example.com\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBSbM8wuD5ab0nHsXaYOqaD3GLLUwmDzSk79Xi/N+H2j test@example.com",
			expectError: false,
		},
		{
			name:        "valid key with comment",
			sshKey:      "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDiYUb9Fy2vlPfO+HwubnshimpVrWPoePyvyN+jPC5gWqZSycjMy6Is2vFVn7oQc72bkY0wZalspT5wUOwKtltSoLpL7vcqGL9zHVw4yjYXtPGIRd3zLpU9wdngevnepPQWTX3LvZTZfmOsrGoMDKIG+Lbmiq/STMuWYecIqMp7tUKRGS8vfAmpu6MsrN9/4UTcdWWXYWJQQn+2nCyMz28jYlWRsKtqFK6owrdZWt8WQnPN+9Upcf2ByQje+0NLnpNrnh+yd2ocuVW9wQYKAZXy7IaTfEJwd5m34sLwkqlZTaBBcmWJU+3RfpYXE763cf3rUoPIGQ8eUEBJ8IdM4vhp user@example.com",
			expectError: false,
		},
		{
			name:        "valid key with options and comment (we don't support options yet)",
			sshKey:      "command=\"echo hello\" ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDiYUb9Fy2vlPfO+HwubnshimpVrWPoePyvyN+jPC5gWqZSycjMy6Is2vFVn7oQc72bkY0wZalspT5wUOwKtltSoLpL7vcqGL9zHVw4yjYXtPGIRd3zLpU9wdngevnepPQWTX3LvZTZfmOsrGoMDKIG+Lbmiq/STMuWYecIqMp7tUKRGS8vfAmpu6MsrN9/4UTcdWWXYWJQQn+2nCyMz28jYlWRsKtqFK6owrdZWt8WQnPN+9Upcf2ByQje+0NLnpNrnh+yd2ocuVW9wQYKAZXy7IaTfEJwd5m34sLwkqlZTaBBcmWJU+3RfpYXE763cf3rUoPIGQ8eUEBJ8IdM4vhp user@example.com",
			expectError: true,
		},
		{
			name:        "empty string",
			sshKey:      "",
			expectError: true,
			errorMsg:    "no valid SSH key found",
		},
		{
			name:        "whitespace only",
			sshKey:      "   \n\t  \n  ",
			expectError: true,
			errorMsg:    "no valid SSH key found",
		},
		{
			name:        "comment only",
			sshKey:      "# This is a comment\n# Another comment",
			expectError: true,
			errorMsg:    "no valid SSH key found",
		},
		{
			name:        "invalid key format",
			sshKey:      "not-a-valid-ssh-key",
			expectError: true,
		},
		{
			name:        "invalid key type",
			sshKey:      "ssh-dss AAAAB3NzaC1kc3MAAACBAOeB...",
			expectError: true,
			errorMsg:    "invalid SSH key type: ssh-dss",
		},
		{
			name:        "unsupported key type",
			sshKey:      "ssh-rsa-cert-v01@openssh.com AAAAB3NzaC1yc2EAAAADAQABAAABgQC7vbqajDhA...",
			expectError: true,
			errorMsg:    "invalid SSH key type: ssh-rsa-cert-v01@openssh.com",
		},
		{
			name:        "malformed key data",
			sshKey:      "ssh-rsa invalid-base64-data",
			expectError: true,
		},
		{
			name:        "type mismatch",
			sshKey:      "ssh-rsa AAAAC3NzaC1lZDI1NTE5AAAAIGomKoH...",
			expectError: true,
			errorMsg:    "parsed SSH key type ssh-ed25519 does not match type in text ssh-rsa",
		},
		{
			name:        "mixed valid and invalid keys",
			sshKey:      "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDiYUb9Fy2vlPfO+HwubnshimpVrWPoePyvyN+jPC5gWqZSycjMy6Is2vFVn7oQc72bkY0wZalspT5wUOwKtltSoLpL7vcqGL9zHVw4yjYXtPGIRd3zLpU9wdngevnepPQWTX3LvZTZfmOsrGoMDKIG+Lbmiq/STMuWYecIqMp7tUKRGS8vfAmpu6MsrN9/4UTcdWWXYWJQQn+2nCyMz28jYlWRsKtqFK6owrdZWt8WQnPN+9Upcf2ByQje+0NLnpNrnh+yd2ocuVW9wQYKAZXy7IaTfEJwd5m34sLwkqlZTaBBcmWJU+3RfpYXE763cf3rUoPIGQ8eUEBJ8IdM4vhp test@example.com\ninvalid-key\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBSbM8wuD5ab0nHsXaYOqaD3GLLUwmDzSk79Xi/N+H2j test@example.com",
			expectError: false,
		},
		{
			name:        "valid key with empty lines and comments",
			sshKey:      "# Comment line\n\nssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDiYUb9Fy2vlPfO+HwubnshimpVrWPoePyvyN+jPC5gWqZSycjMy6Is2vFVn7oQc72bkY0wZalspT5wUOwKtltSoLpL7vcqGL9zHVw4yjYXtPGIRd3zLpU9wdngevnepPQWTX3LvZTZfmOsrGoMDKIG+Lbmiq/STMuWYecIqMp7tUKRGS8vfAmpu6MsrN9/4UTcdWWXYWJQQn+2nCyMz28jYlWRsKtqFK6owrdZWt8WQnPN+9Upcf2ByQje+0NLnpNrnh+yd2ocuVW9wQYKAZXy7IaTfEJwd5m34sLwkqlZTaBBcmWJU+3RfpYXE763cf3rUoPIGQ8eUEBJ8IdM4vhp test@example.com\n# Another comment\n\t\n",
			expectError: false,
		},
		{
			name:        "all invalid keys",
			sshKey:      "invalid-key-1\ninvalid-key-2\nssh-dss AAAAB3NzaC1kc3MAAACBAOeB...",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSSHKey(tt.sshKey)

			if tt.expectError {
				if err == nil {
					t.Errorf("ValidateSSHKey() expected error but got none")
				} else if tt.errorMsg != "" && !strings.ContainsAny(err.Error(), tt.errorMsg) {
					t.Errorf("ValidateSSHKey() error = %v, expected to contain %v", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateSSHKey() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestValidSSHKeyTypes(t *testing.T) {
	expectedTypes := []string{
		"ssh-rsa",
		"ssh-ed25519",
		"ecdsa-sha2-nistp256",
		"ecdsa-sha2-nistp384",
		"ecdsa-sha2-nistp521",
		"sk-ecdsa-sha2-nistp256@openssh.com",
		"sk-ssh-ed25519@openssh.com",
	}

	if len(ValidSSHKeyTypes) != len(expectedTypes) {
		t.Errorf("ValidSSHKeyTypes length = %d, expected %d", len(ValidSSHKeyTypes), len(expectedTypes))
	}

	for _, expectedType := range expectedTypes {
		found := false
		for _, actualType := range ValidSSHKeyTypes {
			if actualType == expectedType {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ValidSSHKeyTypes missing expected type: %s", expectedType)
		}
	}
}

// TestValidateSSHKeyEdgeCases tests edge cases and boundary conditions
func TestValidateSSHKeyEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		sshKey      string
		expectError bool
	}{
		{
			name:        "key with only type",
			sshKey:      "ssh-rsa",
			expectError: true,
		},
		{
			name:        "key with type and empty data",
			sshKey:      "ssh-rsa ",
			expectError: true,
		},
		{
			name:        "key with type and whitespace data",
			sshKey:      "ssh-rsa   \t  ",
			expectError: true,
		},
		{
			name:        "key with multiple spaces between type and data",
			sshKey:      "ssh-rsa    AAAAB3NzaC1yc2EAAAADAQABAAABAQDiYUb9Fy2vlPfO+HwubnshimpVrWPoePyvyN+jPC5gWqZSycjMy6Is2vFVn7oQc72bkY0wZalspT5wUOwKtltSoLpL7vcqGL9zHVw4yjYXtPGIRd3zLpU9wdngevnepPQWTX3LvZTZfmOsrGoMDKIG+Lbmiq/STMuWYecIqMp7tUKRGS8vfAmpu6MsrN9/4UTcdWWXYWJQQn+2nCyMz28jYlWRsKtqFK6owrdZWt8WQnPN+9Upcf2ByQje+0NLnpNrnh+yd2ocuVW9wQYKAZXy7IaTfEJwd5m34sLwkqlZTaBBcmWJU+3RfpYXE763cf3rUoPIGQ8eUEBJ8IdM4vhp test@example.com",
			expectError: false,
		},
		{
			name:        "key with tabs",
			sshKey:      "\tssh-rsa\tAAAAB3NzaC1yc2EAAAADAQABAAABAQDiYUb9Fy2vlPfO+HwubnshimpVrWPoePyvyN+jPC5gWqZSycjMy6Is2vFVn7oQc72bkY0wZalspT5wUOwKtltSoLpL7vcqGL9zHVw4yjYXtPGIRd3zLpU9wdngevnepPQWTX3LvZTZfmOsrGoMDKIG+Lbmiq/STMuWYecIqMp7tUKRGS8vfAmpu6MsrN9/4UTcdWWXYWJQQn+2nCyMz28jYlWRsKtqFK6owrdZWt8WQnPN+9Upcf2ByQje+0NLnpNrnh+yd2ocuVW9wQYKAZXy7IaTfEJwd5m34sLwkqlZTaBBcmWJU+3RfpYXE763cf3rUoPIGQ8eUEBJ8IdM4vhp test@example.com",
			expectError: false,
		},
		{
			name:        "very long line",
			sshKey:      "ssh-rsa " + string(make([]byte, 10000)),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSSHKey(tt.sshKey)

			if tt.expectError {
				if err == nil {
					t.Errorf("ValidateSSHKey() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("ValidateSSHKey() unexpected error = %v", err)
				}
			}
		})
	}
}
