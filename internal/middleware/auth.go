package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
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
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

var (
	cachedKeys   []jwkKey
	cachedKeysAt time.Time
	keysMu       sync.RWMutex
)

func fetchJWKS() ([]jwkKey, error) {
	url := config.Global.Supabase.URL + "/auth/v1/.well-known/jwks.json"
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, err
	}
	return jwks.Keys, nil
}

func getJWKS() ([]jwkKey, error) {
	keysMu.RLock()
	if len(cachedKeys) > 0 && time.Since(cachedKeysAt) < time.Hour {
		keys := cachedKeys
		keysMu.RUnlock()
		return keys, nil
	}
	keysMu.RUnlock()

	keys, err := fetchJWKS()
	if err != nil {
		return nil, err
	}
	keysMu.Lock()
	cachedKeys = keys
	cachedKeysAt = time.Now()
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
			if kid == "" || k.Kid == kid {
				xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
				if err != nil {
					continue
				}
				yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
				if err != nil {
					continue
				}
				return &ecdsa.PublicKey{
					Curve: elliptic.P256(),
					X:     new(big.Int).SetBytes(xBytes),
					Y:     new(big.Int).SetBytes(yBytes),
				}, nil
			}
		}
		return nil, fmt.Errorf("no matching JWKS key for kid=%s", kid)

	default:
		return nil, fmt.Errorf("unsupported alg: %s", alg)
	}
}

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")
		token, err := jwt.Parse(tokenStr, jwtKeyFunc)
		if err != nil || !token.Valid {
			log.Printf("auth: JWT error: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid claims"})
			return
		}

		userID, ok := claims["sub"].(string)
		if !ok || userID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing user id"})
			return
		}

		c.Set("user_id", userID)
		c.Next()
	}
}
