package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/kayawari/graphql-todo/services/auth/internal/password"
)

// login は「メールアドレスが存在しない」と「パスワードが違う」を区別しない
// （design.md §4.1, §6.8-#2）。
//
// 区別するとメールアドレスの存在確認に使われる。レスポンスの内容だけでなく
// 応答時間まで揃える必要があるため、該当ユーザーが居ない場合もダミーハッシュに
// 対して bcrypt 比較を実行する。
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errInvalidRequest, "malformed JSON body")
		return
	}
	req.Email = strings.TrimSpace(req.Email)

	cred, err := h.q.GetCredentialByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 計算を省略すると応答が明確に速くなり、タイミングから
			// メールアドレスの存在有無が漏れる。
			password.VerifyDummy(req.Password)
			writeError(w, http.StatusUnauthorized, errInvalidCredentials, "")
			return
		}
		h.internalError(w, "get credential", err)
		return
	}

	if !password.Verify(cred.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, errInvalidCredentials, "")
		return
	}

	// display_name は auth_db に保存していない（真実源は user_db）。
	// 初回ログイン時の JIT provisioning 用に、ここでは空文字を claim に載せ、
	// user サブグラフ側でフォールバックさせる。
	//
	// TODO: サインアップ以降に displayName を claim へ載せる必要が出たら、
	// user サブグラフに問い合わせるのではなく claim を消す方向で検討する
	// （auth が user に依存すると分離の意味が薄れるため）。
	h.respondWithToken(w, http.StatusOK, cred.ID, cred.Email, "")
}
