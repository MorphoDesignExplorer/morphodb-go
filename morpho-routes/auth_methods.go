package morphoroutes

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"io"
	"slices"
	"time"
)

// Data Types

type ExpirationDetails struct {
	ExpiresAt time.Time `json:"eat"`
	IssuedAt  time.Time `json:"iat"`
}

func (e *ExpirationDetails) Init(duration time.Duration) {
	now := time.Now()
	e.IssuedAt = now
	e.ExpiresAt = now.Add(duration)
}

func (e *ExpirationDetails) Valid() bool {
	return !time.Now().After(e.ExpiresAt) && !time.Now().Before(e.IssuedAt)
}

type ResetSessionToken struct {
	Email      *string            `json:"reset_email"`
Expiration *ExpirationDetails `json:"expires"`
}


// Permission Bit Flags for easily specifying the permissions needed for a route.
//
// Combine permissions needed with a pipe (|).
type PermissionFlags int
const (
	CAN_CREATE 	PermissionFlags = 0b001	// Is the user allowed to create objects?
	CAN_UPDATE 	PermissionFlags = 0b010	// Is the user allowed to modify objects?
	IS_ADMIN 	PermissionFlags = 0b100	// Is the user an administrator?
)

/*
Struct representing the permission an auth token has.
This is meant to be extended in the future.
*/
type Permissions struct {
	Create  bool `json:"create"`
	Update  bool `json:"update"`
	IsAdmin bool `json:"is_admin"`
}

// Is the provided permission enough for the requirement?
// 
// required flags if the permission is required.
// 
// provided flags if the user has the required permission.
func vet(required, provided bool) bool {
	return (!required && !provided) || provided
}

func (p Permissions) HasPermission(flagSet PermissionFlags) bool {
	return vet(CAN_CREATE & flagSet > 0, p.Create) && vet(CAN_UPDATE & flagSet > 0, p.Update) && vet(IS_ADMIN & flagSet > 0, p.IsAdmin)
}

type AuthToken struct {
	Email       *string            `json:"auth_email"`
	Permissions *Permissions       `json:"permissions"`
	Expiration  *ExpirationDetails `json:"expires"`
}

// Token methods

func (t *AuthToken) Valid() bool {
	return t.Email != nil && t.Permissions != nil && t.Expiration != nil && t.Expiration.Valid()
}

func (t *AuthToken) SetExpiration(duration time.Duration) {
	t.Expiration.Init(duration)
}

func (t *ResetSessionToken) Valid() bool {
	return t.Email != nil && t.Expiration != nil && t.Expiration.Valid()
}

func (t *ResetSessionToken) SetExpiration(duration time.Duration) {
	t.Expiration.Init(duration)
}

// Crypto Methods

func GetParameter(keyName string) ([]byte, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	client := ssm.NewFromConfig(cfg)

	result, err := client.GetParameter(context.TODO(), &ssm.GetParameterInput{
		Name: aws.String(keyName),
	})
	if err != nil {
		return nil, err
	}

	return []byte(*result.Parameter.Value), nil
}

func getPasswordSecretParameter() ([]byte, error) {
	return GetParameter("PASS_SECRET")
}

func getEncryptionSecretParameter() ([]byte, error) {
	return GetParameter("ENC_SECRET")
}

type Secrets struct {
	password   []byte	// password salt
	encryption []byte	// encryption secret
}

func (s *Secrets) Init() error {
	var err error

	config, err := GetConfig()
	if err != nil {
		return err
	}

	if config.ENVIRONMENT == "prod" {
		s.encryption, err = getEncryptionSecretParameter()
		if err != nil {
			return err
		}

		s.password, err = getPasswordSecretParameter()
		if err != nil {
			return err
		}

		return nil
	} else {
		// dummy values for a dev environment
		s.password = []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		s.encryption = []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

		return nil
	}
}

var secrets Secrets

func (s *Secrets) EncryptBytes(payload []byte) ([]byte, error) {
	encSecret := s.encryption

	// source for encryption code: https://pkg.go.dev/crypto/cipher@go1.24.3#example-NewGCM-Decrypt
	cipherBlock, err := aes.NewCipher(encSecret)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(cipherBlock)
	if err != nil {
		return nil, err
	}

	ciphertext := aesgcm.Seal(nil, nonce, payload, nil)

	// append nonce/IV to the beginning of the ciphertext
	result := make([]byte, len(nonce))
	copy(result, nonce)
	result = append(result, ciphertext...)

	return result, nil
}

func (s *Secrets) DecryptBytes(payload []byte) ([]byte, error) {
	encSecret := s.encryption

	// source for decryption code: https://pkg.go.dev/crypto/cipher@go1.24.3#example-NewGCM-Encrypt
	cipherBlock, err := aes.NewCipher(encSecret)
	if err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(cipherBlock)
	if err != nil {
		return nil, err
	}

	nonce, ciphertext := payload[:12], payload[12:]

	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

func Btoi(b []byte) (result int) {
	result = 0
	for _, byt := range b {
		result = result << 8
		result += int(byt)
	}
	return
}

func Itob(i int) (result []byte) {
	result = make([]byte, 0)
	for i > 0 || len(result) != 4 {
		var b byte = byte(i)
		result = append(result, b)
		i = i >> 8
	}
	slices.Reverse(result)
	return
}

type Token interface {
	SetExpiration(time.Duration)
}

// A generic method to generate an encrypted and signed JWT.
//
// Returns a Base64-encoded encrypted JWT.
// Returns an error if the JWT signing or encryption fails.
func GenerateToken(s Secrets, token Token, expiresIn time.Duration) ([]byte, error) {
	token.SetExpiration(expiresIn)

	jsonBytes, err := json.Marshal(token)
	if err != nil {
		return nil, err
	}

	encryptedBytes, err := s.EncryptBytes([]byte(jsonBytes))
	if err != nil {
		return nil, err
	}

	byteLength := len(encryptedBytes)
	byteLengthHeader := Itob(byteLength)

	byteLengthHeader = append(byteLengthHeader, encryptedBytes...)

	base64bytes := make([]byte, base64.StdEncoding.EncodedLen(len(byteLengthHeader)))
	base64.StdEncoding.Encode(base64bytes, byteLengthHeader)

	return base64bytes, nil
}

/*
Decodes and verifies an Base64-encoded encrypted token.
If the token is valid, returns the token. Else, returns an error.
*/
func VerifyToken[T any](s Secrets, payload []byte) (token T, err error) {
	decodedBytes := make([]byte, base64.StdEncoding.DecodedLen(len(payload)))
	_, err = base64.StdEncoding.Decode(decodedBytes, payload)

	if err != nil {
		return
	}

	if len(decodedBytes) < 4 {
		return token, fmt.Errorf("The payload is too short.")
	}

	byteLengthHeader := decodedBytes[:4]
	byteLength := Btoi(byteLengthHeader)

	decodedBytes = decodedBytes[4:][:byteLength]

	jsonBytes, err := s.DecryptBytes(decodedBytes)
	if err != nil {
		return token, fmt.Errorf("could not decrypt token: %s", err)
	}

	err = json.Unmarshal(jsonBytes, &token)
	if err != nil {
		return token, err
	}

	return
}
