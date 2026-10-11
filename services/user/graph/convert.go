package graph

import (
	"github.com/kayawari/graphql-todo/services/user/graph/model"
	"github.com/kayawari/graphql-todo/services/user/internal/auth"
	"github.com/kayawari/graphql-todo/services/user/internal/db"
)

// toUserModel は DB の行を GraphQL の型に変換する。
//
// email は user_db に保存されておらず、リクエストの JWT claims からしか解決できない。
// 手元にあるのは常に自分のトークンだけなので、他人の User エンティティを
// どう解決しても email は null にしかならない。
// 「resolver で所有者チェックを書く」ではなく「構造上、他人の値を持ち得ない」
// という守り方になっている（design.md §4.2）。
func toUserModel(u db.User, v *auth.Viewer) *model.User {
	m := &model.User{
		ID:          u.ID.String(),
		DisplayName: u.DisplayName,
		CreatedAt:   u.CreatedAt.Time,
	}
	if v != nil && v.UserID == u.ID && v.Email != "" {
		email := v.Email
		m.Email = &email
	}
	return m
}
