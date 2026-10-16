package record

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrInvalidID rejects identifiers that are not safe single path
// components. The wrap adds which identifier carried it.
var ErrInvalidID = errors.New("invalid identifier")

// idMaxLen bounds identifiers so a full record path stays comfortably
// inside the path limits of both target platforms.
const idMaxLen = 64

// checkID validates one identifier of the given kind (record, evidence,
// resource). Identifiers become single path components under the data
// root, so the accepted set is lowercase letters, digits and hyphens only:
// it can express no separator, no parent reference and (with the
// reserved-name check) no Windows device name, which is what keeps a
// record inside its own directory on every platform.
func checkID(kind, id string) error {
	if !validID(id) {
		return fmt.Errorf("%s id %q: %w", kind, id, ErrInvalidID)
	}
	return nil
}

// validID reports whether id is a safe single path component: 1..64
// characters from [a-z0-9-], not starting or ending with a hyphen, and not
// a reserved device name. Lowercase-only also keeps two different ids from
// colliding on case-insensitive filesystems.
func validID(id string) bool {
	if len(id) == 0 || len(id) > idMaxLen {
		return false
	}
	if id[0] == '-' || id[len(id)-1] == '-' {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return !reservedName(id)
}

// reservedName reports whether the already lowercase-only id collides
// with a reserved device name on Windows (CON, PRN, AUX, NUL, COM1-9,
// LPT1-9). Such a name can make a file unreachable or shadow a device
// even though it is a legal component on other platforms, so it is
// rejected everywhere to keep records portable.
func reservedName(id string) bool {
	switch id {
	case "con", "prn", "aux", "nul":
		return true
	}
	if len(id) == 4 {
		switch id[:3] {
		case "com", "lpt":
			return id[3] >= '1' && id[3] <= '9'
		}
	}
	return false
}

// NewRecordID returns a fresh local record identifier: lowercase RFC 4122
// v4 UUID text, the shape the result contract uses for session
// identities. Every scan and every imported archive gets its own record
// directory named by this id, so two imports of the same source never
// share a directory.
func NewRecordID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("record: generate record id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32], nil
}
