// Package auth はサブグラフ側での JWT 再検証を担う（design.md §6.6）。
//
// Router がすでに検証済みでも、ここで再度検証する。Router を迂回して
// サブグラフのポートへ直接到達された場合、X-User-ID ヘッダはいくらでも詐称できるため、
// 認可の根拠は常に「自分で検証した JWT」とする。
package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrUnauthenticated = errors.New("unauthenticated")

// Viewer は検証済みトークンから取り出した「このリクエストの実行主体」。
type Viewer struct {
	UserID      uuid.UUID
	Email       string
	DisplayName string
	Scopes      []string
}

// Claims は auth サービスが発行する JWT のペイロード（design.md §6.1）。
type Claims struct {
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Scopes      []string `json:"scopes"`

	jwt.RegisteredClaims
}

type ctxKey struct{}

type Verifier struct {
	ctx      context.Context
	jwksURL  string
	issuer   string
	audience string
	log      *slog.Logger

	mu sync.Mutex
	kf keyfunc.Keyfunc
}

func NewVerifier(ctx context.Context, jwksURL, issuer, audience string, log *slog.Logger) *Verifier {
	return &Verifier{
		ctx:      ctx,
		jwksURL:  jwksURL,
		issuer:   issuer,
		audience: audience,
		log:      log,
	}
}

// keyfunc は JWKS クライアントを遅延生成する。
//
// 起動時に取得しないのは、auth サービスの起動を待たずにサブグラフを立ち上げたいため
// （design.md §8.2）。取得に失敗しても記憶せず、次のリクエストで再試行する。
func (v *Verifier) keyfunc() (keyfunc.Keyfunc, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.kf != nil {
		return v.kf, nil
	}
	kf, err := keyfunc.NewDefaultCtx(v.ctx, []string{v.jwksURL})
	if err != nil {
		return nil, err
	}
	v.kf = kf
	return kf, nil
}

// Middleware は Authorization ヘッダの JWT を検証し、成功時のみ Viewer を context に入れる。
//
// 検証に失敗してもここでは 401 を返さず「未認証のまま通す」。
// 可否の判定は resolver 層に委ねる（design.md §6.6）。
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}

		viewer, err := v.verify(raw)
		if err != nil {
			// 理由はログにだけ残す。クライアントへは区別させない（design.md §6.8-#9）。
			v.log.Debug("token verification failed", "err", err)
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), ctxKey{}, viewer)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (v *Verifier) verify(raw string) (*Viewer, error) {
	kf, err := v.keyfunc()
	if err != nil {
		return nil, err
	}

	var claims Claims
	_, err = jwt.ParseWithClaims(raw, &claims, kf.Keyfunc,
		// alg を固定する。alg: none や、公開鍵を HMAC の共通鍵として使わせる
		// アルゴリズム混同攻撃を防ぐ（design.md §6.2 セキュリティ注意 2）。
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return nil, err
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, err
	}

	return &Viewer{
		UserID:      userID,
		Email:       claims.Email,
		DisplayName: claims.DisplayName,
		Scopes:      claims.Scopes,
	}, nil
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

// ViewerFromContext は未認証なら ErrUnauthenticated を返す。
// 認証が必要な resolver は必ず先頭でこれを呼ぶ。
func ViewerFromContext(ctx context.Context) (*Viewer, error) {
	v, ok := ctx.Value(ctxKey{}).(*Viewer)
	if !ok || v == nil {
		return nil, ErrUnauthenticated
	}
	return v, nil
}
