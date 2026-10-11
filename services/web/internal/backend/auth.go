// Package backend は web から auth サービスと Router を呼ぶクライアント。
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

var (
	// ErrInvalidCredentials はログイン失敗。理由（メールが無い / パスワード違い）は
	// auth 側でも区別していない（design.md §4.1）。
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailExists        = errors.New("email already exists")
)

// InputError は利用者に見せてよい入力エラー。
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

type Token struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int    `json:"expiresIn"`
}

type Auth struct {
	baseURL string
	hc      *http.Client
}

func NewAuth(baseURL string, hc *http.Client) *Auth {
	return &Auth{baseURL: baseURL, hc: hc}
}

func (a *Auth) Login(ctx context.Context, email, password string) (*Token, error) {
	return a.post(ctx, "/auth/login", map[string]string{
		"email":    email,
		"password": password,
	})
}

func (a *Auth) Signup(ctx context.Context, email, password, displayName string) (*Token, error) {
	return a.post(ctx, "/auth/signup", map[string]string{
		"email":       email,
		"password":    password,
		"displayName": displayName,
	})
}

func (a *Auth) post(ctx context.Context, path string, body any) (*Token, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := a.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var t Token
		if err := json.NewDecoder(res.Body).Decode(&t); err != nil {
			return nil, err
		}
		return &t, nil
	case http.StatusUnauthorized:
		return nil, ErrInvalidCredentials
	case http.StatusConflict:
		return nil, ErrEmailExists
	case http.StatusBadRequest:
		var e struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Message == "" {
			e.Message = "入力内容を確認してください"
		}
		return nil, &InputError{Msg: e.Message}
	default:
		return nil, fmt.Errorf("auth %s: unexpected status %d", path, res.StatusCode)
	}
}
