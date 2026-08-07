package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/paraview/keylite/internal/model"
)

// Issuer is the iss claim on every token this system mints.
const Issuer = "keylite"

// Errors returned by Verify. Callers map these to the API error codes that
// distinguish an expired session from a revoked one.
var (
	ErrTokenInvalid = errors.New("token is invalid")
	ErrTokenExpired = errors.New("token has expired")
)

// Claims is the token payload. It carries the organization so that a
// downstream system can identify the user and their org from the token
// alone, without a callback (§3.6.1).
type Claims struct {
	Username         string     `json:"username"`
	DisplayName      string     `json:"displayName"`
	Role             model.Role `json:"role"`
	OrganizationID   string     `json:"organizationId,omitempty"`
	OrganizationName string     `json:"organizationName,omitempty"`

	// TokenVersion must match the user's current value. Logout, a password
	// change, and disabling the account each bump that value, which revokes
	// every token issued before it.
	TokenVersion int64 `json:"tokenVersion"`

	jwt.RegisteredClaims
}

// TokenService issues and verifies access tokens.
type TokenService struct {
	secret []byte
}

// NewTokenService returns a service signing with secret.
func NewTokenService(secret []byte) *TokenService {
	return &TokenService{secret: secret}
}

// Issue mints a token for user, valid for ttl.
func (s *TokenService) Issue(user model.User, tokenVersion int64, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(ttl)

	claims := Claims{
		Username:         user.Username,
		DisplayName:      user.DisplayName,
		Role:             user.Role,
		OrganizationID:   user.OrganizationID,
		OrganizationName: user.OrganizationName,
		TokenVersion:     tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			Issuer:    Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Verify parses and validates a token, returning its claims.
//
// It does not check TokenVersion against the database — that is the
// middleware's job, since it requires a user lookup. Verify only proves the
// token is authentic and unexpired.
func (s *TokenService) Verify(raw string) (*Claims, error) {
	claims := &Claims{}

	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		// Reject any algorithm other than the one we sign with. Without this
		// check a token could claim "alg":"none" and be accepted unsigned.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.secret, nil
	},
		jwt.WithIssuer(Issuer),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %w", ErrTokenInvalid, err)
	}

	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: missing subject", ErrTokenInvalid)
	}

	return claims, nil
}
