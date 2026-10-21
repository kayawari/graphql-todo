// Package refresh はリフレッシュトークンの発行・ローテーション・失効を担う（design.md §6.9）。
//
// トークンは不透明な乱数で、auth_db にはその SHA-256 ハッシュだけを置く。
// 1 回使ったら新しい値に差し替え、使用済みの値が再び来たら盗用とみなして
// そのファミリー（同じログインから派生したトークン列）を丸ごと失効させる。
package refresh

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kayawari/graphql-todo/services/auth/internal/db"
)

// ErrInvalid は不明・期限切れ・再利用のいずれか。
// 呼び出し側（HTTP handler）には理由を区別させない（design.md §6.8-#9）。
var ErrInvalid = errors.New("invalid refresh token")

// ReuseError は使用済みトークンの再提示（盗用の疑い）。ファミリーは失効済み。
// errors.Is(err, ErrInvalid) が成り立つので、応答は他の失敗と同じにできる。
// ログに残すための ID だけを持ち、トークンの値は持たない。
type ReuseError struct {
	UserID   uuid.UUID
	FamilyID uuid.UUID
}

func (e *ReuseError) Error() string {
	return fmt.Sprintf("refresh token reused: user %s family %s", e.UserID, e.FamilyID)
}

func (e *ReuseError) Unwrap() error { return ErrInvalid }

// Store は refresh_tokens テーブルへの操作をまとめる。
type Store struct {
	pool   *pgxpool.Pool
	q      *db.Queries
	ttl    time.Duration
	maxAge time.Duration
	grace  time.Duration
}

// New は Store を作る。ttl は 1 回の使用で延びる期限、maxAge はログインからの絶対上限、
// grace は並行リクエストを再利用とみなさない猶予。
func New(pool *pgxpool.Pool, ttl, maxAge, grace time.Duration) *Store {
	return &Store{
		pool:   pool,
		q:      db.New(pool),
		ttl:    ttl,
		maxAge: maxAge,
		grace:  grace,
	}
}

// Issued は発行したトークンと、その期限。
type Issued struct {
	Token     string
	ExpiresAt time.Time
}

// Issue はログイン / サインアップ時に新しいファミリーを始める。
func (s *Store) Issue(ctx context.Context, userID uuid.UUID) (Issued, error) {
	now := time.Now()
	familyExpiresAt := now.Add(s.maxAge)
	return s.insert(ctx, s.q, uuid.New(), userID, now, familyExpiresAt)
}

// Rotated は Rotate の結果。猶予内の再提示では New が空になる
// （アクセストークンだけを出し直し、リフレッシュトークンは差し替えない）。
type Rotated struct {
	UserID uuid.UUID
	Email  string
	New    *Issued
}

// Rotate は提示されたトークンを使用済みにし、同じファミリーで新しいトークンを発行する。
func (s *Store) Rotate(ctx context.Context, token string) (Rotated, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Rotated{}, fmt.Errorf("begin: %w", err)
	}
	// Commit 後の Rollback は何もしない。途中で return したときの後始末用。
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	row, err := q.GetRefreshTokenForUpdate(ctx, hash(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Rotated{}, ErrInvalid
		}
		return Rotated{}, fmt.Errorf("get refresh token: %w", err)
	}

	now := time.Now()
	t := state{expiresAt: row.ExpiresAt.Time}
	if row.UsedAt.Valid {
		t.usedAt = &row.UsedAt.Time
	}

	switch decide(t, now, s.grace) {
	case actionReject:
		return Rotated{}, ErrInvalid

	case actionRevokeFamily:
		if err := q.DeleteRefreshTokenFamily(ctx, row.FamilyID); err != nil {
			return Rotated{}, fmt.Errorf("delete family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Rotated{}, fmt.Errorf("commit: %w", err)
		}
		return Rotated{}, &ReuseError{UserID: row.UserID, FamilyID: row.FamilyID}

	case actionAccessOnly:
		// 行は変えないのでコミットは不要（defer の Rollback でロックを放す）。
		return Rotated{UserID: row.UserID, Email: row.Email}, nil
	}

	// actionRotate
	if err := q.MarkRefreshTokenUsed(ctx, db.MarkRefreshTokenUsedParams{
		ID:     row.ID,
		UsedAt: timestamptz(now),
	}); err != nil {
		return Rotated{}, fmt.Errorf("mark used: %w", err)
	}
	issued, err := s.insert(ctx, q, row.FamilyID, row.UserID, now, row.FamilyExpiresAt.Time)
	if err != nil {
		return Rotated{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Rotated{}, fmt.Errorf("commit: %w", err)
	}
	return Rotated{UserID: row.UserID, Email: row.Email, New: &issued}, nil
}

// Revoke はログアウト。提示されたトークンのファミリーを消す。
// 該当が無くてもエラーにしない（トークンが有効だったかを呼び出し側に漏らさない）。
func (s *Store) Revoke(ctx context.Context, token string) error {
	if err := s.q.DeleteRefreshTokenFamilyByHash(ctx, hash(token)); err != nil {
		return fmt.Errorf("delete family by hash: %w", err)
	}
	return nil
}

// Cleanup はファミリーの上限を過ぎた行を消す。ログイン時に呼び、cron を置かずに済ませる。
func (s *Store) Cleanup(ctx context.Context, userID uuid.UUID) error {
	if err := s.q.DeleteExpiredRefreshTokens(ctx, userID); err != nil {
		return fmt.Errorf("delete expired refresh tokens: %w", err)
	}
	return nil
}

func (s *Store) insert(ctx context.Context, q *db.Queries, familyID, userID uuid.UUID, now, familyExpiresAt time.Time) (Issued, error) {
	token, err := generate()
	if err != nil {
		return Issued{}, err
	}
	expiresAt := slide(now, s.ttl, familyExpiresAt)
	if err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		FamilyID:        familyID,
		UserID:          userID,
		TokenHash:       hash(token),
		ExpiresAt:       timestamptz(expiresAt),
		FamilyExpiresAt: timestamptz(familyExpiresAt),
	}); err != nil {
		return Issued{}, fmt.Errorf("create refresh token: %w", err)
	}
	return Issued{Token: token, ExpiresAt: expiresAt}, nil
}

// ---- 判定（DB に依存しない部分。テストはここを見る）----

type state struct {
	expiresAt time.Time
	usedAt    *time.Time
}

type action int

const (
	actionRotate       action = iota // 正常。差し替える
	actionAccessOnly                 // 猶予内の再提示。アクセストークンだけ出し直す
	actionRevokeFamily               // 猶予後の再利用。盗用とみなしてファミリーごと失効
	actionReject                     // 期限切れ
)

func decide(t state, now time.Time, grace time.Duration) action {
	if t.usedAt != nil {
		// 期限より先に見る。期限切れ直前に盗まれた値でも、再利用は再利用として扱う。
		if now.Sub(*t.usedAt) <= grace {
			return actionAccessOnly
		}
		return actionRevokeFamily
	}
	if !now.Before(t.expiresAt) {
		return actionReject
	}
	return actionRotate
}

// slide は使うたびに延びる期限を、ファミリーの上限で切り詰める。
func slide(now time.Time, ttl time.Duration, familyExpiresAt time.Time) time.Time {
	if e := now.Add(ttl); e.Before(familyExpiresAt) {
		return e
	}
	return familyExpiresAt
}

// ---- トークン値 ----

func generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
