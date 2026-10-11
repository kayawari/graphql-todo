package graph

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kayawari/graphql-todo/services/todo/internal/service"
)

var errInvalidCursor = errors.New("invalid cursor")

// カーソルは "created_at(RFC3339Nano)|id" の base64url（design.md §4.6）。
// クライアントにとっては不透明な文字列として扱わせる。
func encodeCursor(c service.Cursor) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (*service.Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, errInvalidCursor
	}
	ts, id, ok := strings.Cut(string(b), "|")
	if !ok {
		return nil, errInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, errInvalidCursor
	}
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, errInvalidCursor
	}
	return &service.Cursor{CreatedAt: t, ID: uid}, nil
}
