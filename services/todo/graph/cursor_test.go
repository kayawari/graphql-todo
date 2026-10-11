package graph

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kayawari/graphql-todo/services/todo/internal/service"
)

func TestCursorRoundTrip(t *testing.T) {
	want := service.Cursor{
		// PostgreSQL の timestamptz はマイクロ秒精度。端数が落ちないことを確認する。
		CreatedAt: time.Date(2026, 10, 12, 9, 30, 0, 123456000, time.UTC),
		ID:        uuid.New(),
	}
	got, err := decodeCursor(encodeCursor(want))
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	for _, s := range []string{
		"",
		"!!!",
		"bm8tc2VwYXJhdG9y",             // "no-separator"
		"bm90LWEtdGltZXxub3QtYS11dWlk", // "not-a-time|not-a-uuid"
		"MjAyNi0xMC0xMlQwMDowMDowMFp8bm90LWEtdXVpZA", // 時刻は正しいが UUID が不正
	} {
		if _, err := decodeCursor(s); err == nil {
			t.Errorf("decodeCursor(%q): want error", s)
		}
	}
}
