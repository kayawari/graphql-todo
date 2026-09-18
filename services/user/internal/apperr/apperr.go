// Package apperr は GraphQL エラーを extensions.code 付きで組み立てる（design.md §9.2）。
package apperr

import (
	"log/slog"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

func coded(code, msg string) error {
	return &gqlerror.Error{
		Message:    msg,
		Extensions: map[string]any{"code": code},
	}
}

func Unauthenticated() error {
	return coded("UNAUTHENTICATED", "unauthenticated")
}

// NotFound は「存在しない」と「他人のもの」の両方に使う。
// FORBIDDEN を返すと「その ID は存在する」ことが漏れる（design.md §6.7-2）。
func NotFound() error {
	return coded("NOT_FOUND", "not found")
}

func BadUserInput(msg string) error {
	return coded("BAD_USER_INPUT", msg)
}

// Internal は詳細をログにだけ残し、クライアントには何も伝えない。
func Internal(log *slog.Logger, op string, err error) error {
	log.Error("internal error", "op", op, "err", err)
	return coded("INTERNAL_SERVER_ERROR", "internal server error")
}
