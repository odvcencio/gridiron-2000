// Package v1transport implements the private, read-only HTTP trust boundary
// for the Gridiron HQ commissioner-summary v1 contract.
package v1transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ProviderPath       = "/api/internal/v1/commissioner-summary"
	HeaderKeyID        = "X-Gridiron-HQ-Key-ID"
	HeaderTimestamp    = "X-Gridiron-HQ-Timestamp"
	HeaderSignature    = "X-Gridiron-HQ-Signature"
	HeaderRequestID    = "X-Request-ID"
	EmptyBodySHA256    = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	MaxResponseBytes   = 256 << 10
	MaxClockSkew       = 30 * time.Second
	minimumSecretBytes = 32
)

var (
	keyIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	signaturePattern = regexp.MustCompile(`^sha256=[0-9a-f]{64}$`)
	timestampPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	requestIDPattern = regexp.MustCompile(`^req_[A-Za-z0-9_-]{8,64}$`)
	dummySecret      = []byte("gridiron-hq-unknown-key-dummy-secret")
)

type Credentials struct {
	keyID  string
	secret []byte
}

func NewCredentials(keyID string, secret []byte) (Credentials, error) {
	if !keyIDPattern.MatchString(keyID) {
		return Credentials{}, errors.New("HQ key ID must be a bounded opaque token")
	}
	if len(secret) < minimumSecretBytes || len(secret) > 4096 {
		return Credentials{}, errors.New("HQ secret must contain between 32 and 4096 bytes")
	}
	copySecret := append([]byte(nil), secret...)
	return Credentials{keyID: keyID, secret: copySecret}, nil
}

func (c Credentials) KeyID() string { return c.keyID }

func (c Credentials) valid() bool {
	return keyIDPattern.MatchString(c.keyID) && len(c.secret) >= minimumSecretBytes && len(c.secret) <= 4096
}

func CanonicalInput(timestamp string) string {
	return http.MethodGet + "\n" + ProviderPath + "\n" + timestamp + "\n" + EmptyBodySHA256
}

func sign(secret []byte, timestamp string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(CanonicalInput(timestamp)))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func applySignature(request *http.Request, credentials Credentials, now time.Time) error {
	if request == nil || !credentials.valid() {
		return errors.New("HQ signing input is misconfigured")
	}
	timestamp := strconv.FormatInt(now.UTC().Unix(), 10)
	request.Header.Set(HeaderKeyID, credentials.keyID)
	request.Header.Set(HeaderTimestamp, timestamp)
	request.Header.Set(HeaderSignature, sign(credentials.secret, timestamp))
	return nil
}

type keyring map[string][]byte

func newKeyring(credentials []Credentials) (keyring, error) {
	if len(credentials) == 0 {
		return nil, errors.New("HQ provider requires at least one credential")
	}
	ring := make(keyring, len(credentials))
	for _, credential := range credentials {
		if !credential.valid() {
			return nil, errors.New("HQ provider credential is invalid")
		}
		if _, exists := ring[credential.keyID]; exists {
			return nil, fmt.Errorf("duplicate HQ key ID %q", credential.keyID)
		}
		ring[credential.keyID] = append([]byte(nil), credential.secret...)
	}
	return ring, nil
}

func (k keyring) authorize(request *http.Request, now time.Time) bool {
	keyID, keyOK := singleHeader(request.Header, HeaderKeyID)
	timestamp, timestampOK := singleHeader(request.Header, HeaderTimestamp)
	suppliedSignature, signatureOK := singleHeader(request.Header, HeaderSignature)
	shapeOK := keyOK && timestampOK && signatureOK && keyIDPattern.MatchString(keyID) &&
		timestampPattern.MatchString(timestamp) && signaturePattern.MatchString(suppliedSignature)

	seconds, parseErr := strconv.ParseInt(timestamp, 10, 64)
	if parseErr != nil || strconv.FormatInt(seconds, 10) != timestamp {
		shapeOK = false
	}
	lower := now.UTC().Add(-MaxClockSkew).Unix()
	upper := now.UTC().Add(MaxClockSkew).Unix()
	if seconds < lower || seconds > upper {
		shapeOK = false
	}

	secret, known := k[keyID]
	if !known {
		secret = dummySecret
	}
	expected := sign(secret, timestamp)
	validMAC := hmac.Equal([]byte(expected), []byte(suppliedSignature))
	return shapeOK && known && validMAC
}

func singleHeader(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	returnSingle := len(values) == 1 && values[0] != "" && !strings.Contains(values[0], ",")
	if !returnSingle {
		return "", false
	}
	return values[0], true
}

func safeRequestID(value string) string {
	if requestIDPattern.MatchString(value) {
		return value
	}
	return "req_000000000000000000000000"
}
