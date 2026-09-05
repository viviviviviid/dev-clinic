package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coding-tutor/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

var (
	ErrIdentityNotAllowed = errors.New("token identity is not allowed")
	cachedKeys            []jwkKey
	cachedKeysAt          time.Time
	cachedKeysURL         string
	keysMu                sync.RWMutex
)

const maxJWKSResponseBytes = 1 << 20

var jwksHTTPClient = &http.Client{Timeout: 5 * time.Second}

func fetchJWKS() ([]jwkKey, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.Global.Supabase.URL), "/")
	if baseURL == "" {
		return nil, errors.New("supabase URL is not configured")
	}
	url := baseURL + "/auth/v1/.well-known/jwks.json"
	resp, err := jwksHTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks endpoint returned %s", resp.Status)
	}
	var jwks jwksResponse
	reader := io.LimitReader(resp.Body, maxJWKSResponseBytes+1)
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&jwks); err != nil {
		return nil, err
	}
	if len(jwks.Keys) == 0 {
		return nil, errors.New("jwks endpoint returned no keys")
	}
	return jwks.Keys, nil
}

func getJWKS() ([]jwkKey, error) {
	keysURL := strings.TrimRight(strings.TrimSpace(config.Global.Supabase.URL), "/")
	keysMu.RLock()
	if len(cachedKeys) > 0 && cachedKeysURL == keysURL && time.Since(cachedKeysAt) < time.Hour {
		keys := append([]jwkKey(nil), cachedKeys...)
		keysMu.RUnlock()
		return keys, nil
	}
	keysMu.RUnlock()

	keys, err := fetchJWKS()
	if err != nil {
		return nil, err
	}
	keysMu.Lock()
	cachedKeys = append([]jwkKey(nil), keys...)
	cachedKeysAt = time.Now()
	cachedKeysURL = keysURL
	keysMu.Unlock()
	return keys, nil
}

// JWTKeyFunc is exported for reuse in other packages (e.g. lsp proxy).
func JWTKeyFunc(token *jwt.Token) (interface{}, error) {
	return jwtKeyFunc(token)
}

func jwtKeyFunc(token *jwt.Token) (interface{}, error) {
	alg, _ := token.Header["alg"].(string)
	kid, _ := token.Header["kid"].(string)

	switch alg {
	case "HS256":
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected method for HS256")
		}
		if strings.TrimSpace(config.Global.Supabase.JWTSecret) == "" {
			return nil, errors.New("supabase JWT secret is not configured")
		}
		return []byte(config.Global.Supabase.JWTSecret), nil

	case "ES256":
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected method for ES256")
		}
		keys, err := getJWKS()
		if err != nil {
			return nil, fmt.Errorf("jwks fetch error: %w", err)
		}
		for _, k := range keys {
			if k.Kid == kid && k.Kty == "EC" && k.Alg == "ES256" && k.Crv == "P-256" {
				xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
				if err != nil {
					continue
				}
				yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
				if err != nil {
					continue
				}
				curve := elliptic.P256()
				x := new(big.Int).SetBytes(xBytes)
				y := new(big.Int).SetBytes(yBytes)
				if !curve.IsOnCurve(x, y) {
					continue
				}
				return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
			}
		}
		return nil, fmt.Errorf("no matching JWKS key for kid=%s", kid)

	default:
		return nil, fmt.Errorf("unsupported alg: %s", alg)
	}
}

// ValidateToken verifies a Supabase access token and returns its subject.
// Both HTTP middleware and WebSocket handshakes use this function so that the
// local backend has one authentication policy.
func ValidateToken(tokenString string) (string, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return "", errors.New("token is empty")
	}

	claims := jwt.MapClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"HS256", "ES256"}),
		jwt.WithJSONNumber(),
	)
	token, err := parser.ParseWithClaims(tokenString, claims, jwtKeyFunc)
	if err != nil || token == nil || !token.Valid {
		if err == nil {
			err = errors.New("token is invalid")
		}
		return "", err
	}

	expectedIssuer := strings.TrimRight(strings.TrimSpace(config.Global.Supabase.URL), "/") + "/auth/v1"
	if expectedIssuer == "/auth/v1" || !claims.VerifyIssuer(expectedIssuer, true) {
		return "", errors.New("invalid token issuer")
	}
	if !claims.VerifyAudience("authenticated", true) {
		return "", errors.New("invalid token audience")
	}
	role, ok := claims["role"].(string)
	if !ok || role != "authenticated" {
		return "", errors.New("invalid token role")
	}
	if _, ok := claims["exp"]; !ok || !claims.VerifyExpiresAt(time.Now().Unix(), true) {
		return "", errors.New("token is expired or missing expiration")
	}
	userID, ok := claims["sub"].(string)
	if !ok || strings.TrimSpace(userID) == "" {
		return "", errors.New("missing token subject")
	}
	email, _ := claims["email"].(string)
	if !isAllowedUser(userID, email) {
		return "", ErrIdentityNotAllowed
	}

	return userID, nil
}

func isAllowedUser(userID, email string) bool {
	configured := false
	for _, allowedUserID := range []string{os.Getenv("ALLOWED_USER_ID"), os.Getenv("ALLOWED_USER_IDS")} {
		if strings.TrimSpace(allowedUserID) != "" {
			configured = true
		}
		for _, candidate := range strings.FieldsFunc(allowedUserID, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		}) {
			if candidate == userID {
				return true
			}
		}
	}
	for _, allowedEmail := range []string{os.Getenv("ALLOWED_USER_EMAIL"), os.Getenv("ALLOWED_USER_EMAILS")} {
		if strings.TrimSpace(allowedEmail) != "" {
			configured = true
		}
		for _, candidate := range strings.FieldsFunc(allowedEmail, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		}) {
			if strings.EqualFold(candidate, strings.TrimSpace(email)) {
				return true
			}
		}
	}
	return !configured
}

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}

		tokenStr := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		userID, err := ValidateToken(tokenStr)
		if err != nil {
			log.Printf("auth: JWT error: %v", err)
			if errors.Is(err, ErrIdentityNotAllowed) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "이 Google 계정은 clinic에서 허용되지 않았습니다.", "code": "account_not_allowed"})
			} else {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "로그인 정보를 확인하지 못했습니다. 로그인 정보를 갱신하거나 다시 로그인해 주세요.", "code": "invalid_token"})
			}
			return
		}

		c.Set("user_id", userID)
		c.Next()
	}
}
