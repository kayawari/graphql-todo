package refresh

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDecide(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	grace := 10 * time.Second
	at := func(d time.Duration) *time.Time {
		t := now.Add(d)
		return &t
	}

	tests := []struct {
		name string
		in   state
		want action
	}{
		{"未使用・期限内は差し替える", state{expiresAt: now.Add(time.Hour)}, actionRotate},
		{"未使用・期限ちょうどは拒否", state{expiresAt: now}, actionReject},
		{"未使用・期限切れは拒否", state{expiresAt: now.Add(-time.Second)}, actionReject},
		{"使用から猶予内はアクセストークンだけ", state{expiresAt: now.Add(time.Hour), usedAt: at(-5 * time.Second)}, actionAccessOnly},
		{"使用から猶予ちょうどもアクセストークンだけ", state{expiresAt: now.Add(time.Hour), usedAt: at(-grace)}, actionAccessOnly},
		{"猶予を過ぎた再利用はファミリーごと失効", state{expiresAt: now.Add(time.Hour), usedAt: at(-11 * time.Second)}, actionRevokeFamily},
		{"期限切れでも再利用は再利用として扱う", state{expiresAt: now.Add(-time.Hour), usedAt: at(-time.Minute)}, actionRevokeFamily},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decide(tt.in, now, grace); got != tt.want {
				t.Errorf("decide() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSlide(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	ttl := 14 * 24 * time.Hour

	tests := []struct {
		name            string
		familyExpiresAt time.Time
		want            time.Time
	}{
		{"上限まで余裕があれば ttl だけ延びる", now.Add(30 * 24 * time.Hour), now.Add(ttl)},
		{"上限が近ければ上限で切り詰める", now.Add(3 * 24 * time.Hour), now.Add(3 * 24 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slide(now, ttl, tt.familyExpiresAt); !got.Equal(tt.want) {
				t.Errorf("slide() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateIsUniqueAndHashIsStable(t *testing.T) {
	a, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	b, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("generate() returned the same token twice")
	}
	if got := len(hash(a)); got != 32 {
		t.Errorf("len(hash) = %d, want 32", got)
	}
	if string(hash(a)) != string(hash(a)) {
		t.Error("hash() is not deterministic")
	}
}

func TestReuseErrorIsInvalid(t *testing.T) {
	var err error = &ReuseError{UserID: uuid.New(), FamilyID: uuid.New()}
	if !errors.Is(err, ErrInvalid) {
		t.Error("errors.Is(ReuseError, ErrInvalid) = false, want true")
	}
}
