package secrets

import "encoding/hex"

// MTProtoSecret is a 16-byte random value used as a Teleproxy secret.
type MTProtoSecret [16]byte

// Hex returns the secret as a lowercase 32-character hex string.
func (s MTProtoSecret) Hex() string {
	return hex.EncodeToString(s[:])
}

// MaxActiveSecrets is the maximum number of active Teleproxy secrets,
// enforced by Teleproxy's engine limit.
const MaxActiveSecrets = 16

// UserSecret pairs a user label with their MTProto secret.
type UserSecret struct {
	Label  string
	Secret MTProtoSecret
}
