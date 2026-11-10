package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Signer struct {
	secret []byte
	ttl    time.Duration
}

func NewSigner(secret string) *Signer {
	return &Signer{secret: []byte(secret), ttl: 24 * time.Hour}
}

func HashPassword(password string) (string, error) {
	buf, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type accessClaims struct {
	jwt.RegisteredClaims
	Type string `json:"typ"`
}

func (s *Signer) Issue(accountID string, now time.Time) (string, error) {
	claims := accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accountID,
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Type: "access",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *Signer) Parse(raw string) (string, error) {
	claims := &accessClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil || !parsed.Valid || claims.Type != "access" || claims.Subject == "" {
		return "", errors.New("invalid token")
	}
	return claims.Subject, nil
}
