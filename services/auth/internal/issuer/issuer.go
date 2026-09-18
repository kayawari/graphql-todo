// Package issuer は JWT の発行と JWKS の配信を担う。
// 秘密鍵を持つのはこのサービスだけで、他サービスは JWKS 経由の公開鍵で検証する
// （design.md §6.1, §6.2）。
package issuer

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims は発行する JWT のペイロード（design.md §6.1）。
type Claims struct {
	Email string `json:"email"`
	// JIT provisioning の初期値としてのみ使う。発行時点のスナップショットなので、
	// プロビジョニング後は user_db 側が真実源になる（design.md §6.4）。
	DisplayName string   `json:"display_name"`
	Scopes      []string `json:"scopes"`

	jwt.RegisteredClaims
}

type Issuer struct {
	priv     *rsa.PrivateKey
	kid      string
	issuer   string
	audience string
	ttl      time.Duration
}

func New(pemPath, iss, audience string, ttl time.Duration) (*Issuer, error) {
	priv, err := loadPrivateKey(pemPath)
	if err != nil {
		return nil, fmt.Errorf("load private key: %w", err)
	}
	return &Issuer{
		priv:     priv,
		kid:      thumbprint(&priv.PublicKey),
		issuer:   iss,
		audience: audience,
		ttl:      ttl,
	}, nil
}

// TTL はレスポンスの expiresIn に載せるための値。
func (i *Issuer) TTL() time.Duration { return i.ttl }

// KeyID は起動ログに出して、ローテーションの前後を追えるようにする。
func (i *Issuer) KeyID() string { return i.kid }

func (i *Issuer) Issue(userID uuid.UUID, email, displayName string) (string, error) {
	now := time.Now()
	claims := Claims{
		Email:       email,
		DisplayName: displayName,
		Scopes:      []string{"todo:read", "todo:write"},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings{i.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
			ID:        uuid.NewString(),
		},
	}

	t := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// 検証側はこの kid で JWKS から鍵を引く。
	// これが無いと鍵ローテーション中にどちらの鍵で署名されたか判別できない。
	t.Header["kid"] = i.kid

	return t.SignedString(i.priv)
}

// jwk は JWKS の 1 エントリ。
// 秘密鍵の構造体をそのまま marshal せず、公開部分だけを明示的に組み立てる
// （design.md §6.2 セキュリティ注意 3）。
type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKS は GET /.well-known/jwks.json のレスポンスボディを返す。
//
// 鍵ローテーション（design.md §6.2）を実装するときは、ここで旧鍵と新鍵の
// 両方を返す期間を設ける。今は 1 鍵のみ。
func (i *Issuer) JWKS() ([]byte, error) {
	pub := &i.priv.PublicKey
	return json.Marshal(struct {
		Keys []jwk `json:"keys"`
	}{
		Keys: []jwk{{
			Kty: "RSA",
			Use: "sig",
			Alg: "RS256",
			Kid: i.kid,
			N:   b64(pub.N.Bytes()),
			E:   b64(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
}

// thumbprint は RFC 7638 の JWK Thumbprint を kid として使う。
// 鍵から決定的に導出されるため、サービス間で採番を調整する必要がない。
func thumbprint(pub *rsa.PublicKey) string {
	// RFC 7638: 必須メンバーのみ、キーは辞書順、空白なし。
	canonical := fmt.Sprintf(`{"e":"%s","kty":"RSA","n":"%s"}`,
		b64(big.NewInt(int64(pub.E)).Bytes()),
		b64(pub.N.Bytes()),
	)
	sum := sha256.Sum256([]byte(canonical))
	return b64(sum[:])
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// 公開鍵はファイルに保存せず、秘密鍵から導出する。
// 別々に保存すると「秘密鍵は替えたが配信中の公開鍵が古い」が起こり得る
// （design.md §6.2 鍵の生成と保管）。
func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", path)
	}

	switch block.Type {
	case "PRIVATE KEY": // PKCS#8（openssl genpkey の既定）
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("not an RSA key: %T", key)
		}
		return rsaKey, nil
	case "RSA PRIVATE KEY": // PKCS#1（openssl genrsa）
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported PEM type %q", block.Type)
	}
}
