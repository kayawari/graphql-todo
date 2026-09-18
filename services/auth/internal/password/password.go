// Package password は bcrypt によるハッシュ化と検証を扱う。
package password

import (
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// design.md §2 の技術選定どおり cost 12。
// 1 回あたり 200〜300ms の CPU を占有するため、auth を独立サービスにしている
// （同居させるとプロフィール読み取りのレイテンシを巻き添えにする / ADR 0001 §2.2-D4）。
const cost = 12

func Hash(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	return string(h), err
}

func Verify(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// dummyHash は「メールアドレスが存在しない」ケース用のダミー。
// 起動時ではなく初回利用時に 1 度だけ生成する。
var dummyHash = sync.OnceValue(func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing-equalization"), cost)
	if err != nil {
		// cost が範囲内である限り失敗しない
		panic(err)
	}
	return h
})

// VerifyDummy は該当ユーザーが存在しない場合に呼ぶ。
//
// 比較を省略すると応答時間が明確に短くなり、タイミングからメールアドレスの
// 存在有無が漏れる。必ず同じだけ計算してから失敗させる（design.md §4.1）。
func VerifyDummy(plain string) {
	_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(plain))
}
