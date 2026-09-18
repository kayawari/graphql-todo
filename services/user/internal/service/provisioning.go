// Package service は user サブグラフのドメインロジックを持つ。
package service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kayawari/jikken/graphql-todo/services/user/internal/auth"
	"github.com/kayawari/jikken/graphql-todo/services/user/internal/db"
)

// DefaultDisplayName は JWT の display_name claim が空のときの初期値。
//
// login は display_name を claim に載せない（auth_db に保存していないため）。
// signup 直後に me を呼ぶ経路では claim に値が入るが、いったんログアウトして
// login だけした場合は空になる。
const DefaultDisplayName = "user"

var ErrNotFound = errors.New("user not found")

type UserService struct {
	q *db.Queries
}

func New(q *db.Queries) *UserService {
	return &UserService{q: q}
}

// Get は指定 ID のプロフィールを取得する。**プロビジョニングは行わない。**
//
// entity resolver（FindUserByID）は Router から任意の representation で
// 呼ばれ得るため、こちらを使う。ここで「無ければ作る」をやると、
// users テーブルに任意の UUID の行を作らせる書き込み経路になる
// （design.md §6.4, §6.8-#11）。
func (s *UserService) Get(ctx context.Context, id uuid.UUID) (db.User, error) {
	u, err := s.q.GetUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, ErrNotFound
	}
	return u, err
}

// UpdateDisplayName は viewer 自身の displayName を更新する。
// 以後 user_db がこの値の真実源になり、JWT の claim は参照しない。
func (s *UserService) UpdateDisplayName(ctx context.Context, id uuid.UUID, name string) (db.User, error) {
	u, err := s.q.UpdateDisplayName(ctx, db.UpdateDisplayNameParams{
		ID:          id,
		DisplayName: name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, ErrNotFound
	}
	return u, err
}

// GetOrProvision は viewer 自身のプロフィール行を返し、無ければ JIT provisioning で作る
// （design.md §6.4）。
//
// 呼んでよいのは viewer 自身に対してだけ。他人の ID では絶対に呼ばないこと。
//
// 利用できるクエリ:
//
//	s.q.GetUser(ctx, id) (db.User, error)
//	    行が無ければ pgx.ErrNoRows
//
//	s.q.ProvisionUser(ctx, db.ProvisionUserParams{ID, DisplayName}) (db.User, error)
//	    INSERT ... ON CONFLICT (id) DO NOTHING RETURNING *
//	    競合して 1 行も挿入されなかった場合も pgx.ErrNoRows
func (s *UserService) GetOrProvision(ctx context.Context, v *auth.Viewer) (db.User, error) {
	// 1. まず PK の SELECT。2 回目以降のリクエストはすべてここで終わる。
	//    毎回 INSERT を撃たないのは、書き込みの往復を避けるため。
	u, err := s.q.GetUser(ctx, v.UserID)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, err
	}

	// 2. 初回アクセス。JWT の claims を初期値にして作る。
	u, err = s.q.ProvisionUser(ctx, db.ProvisionUserParams{
		ID:          v.UserID,
		DisplayName: initialDisplayName(v.DisplayName),
	})
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, err
	}

	// 3. ON CONFLICT DO NOTHING は、競合して 1 行も挿入されなかった場合も
	//    pgx.ErrNoRows を返す。つまりここに来るのは「同時に来た別のリクエストが
	//    先に作った」ケース。エラーではないので読み直す。
	//
	//    ここを err として扱うと、同時に 2 リクエストが来たとき片方が必ず失敗する。
	return s.Get(ctx, v.UserID)
}

// initialDisplayName は claim を DB の制約に収まる形に整える。
//
// login は display_name を claim に載せない（auth_db に保存していないため）。
// users.display_name には CHECK (char_length BETWEEN 1 AND 50) があるので、
// 空文字のまま INSERT すると制約違反になる。
func initialDisplayName(claim string) string {
	name := strings.TrimSpace(claim)
	if name == "" {
		return DefaultDisplayName
	}
	if r := []rune(name); len(r) > 50 {
		return string(r[:50])
	}
	return name
}
