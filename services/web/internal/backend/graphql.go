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
	// ErrUnauthenticated はトークンが無い・期限切れ・不正のいずれか。
	// Router は require_authentication: true なので 401 で返してくる。
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrNotFound は「存在しない」と「他人のもの」の両方（design.md §6.7-2）。
	ErrNotFound = errors.New("not found")
)

type GraphQL struct {
	url string
	hc  *http.Client
}

func NewGraphQL(url string, hc *http.Client) *GraphQL {
	return &GraphQL{url: url, hc: hc}
}

type gqlError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code string `json:"code"`
		// Router はサブグラフのエラーを extensions.errors に包んで返す。
		Errors []gqlError `json:"errors"`
	} `json:"extensions"`
}

// Do はクエリを実行し、data を out にデコードする。
//
// 認可の判断はしない。token をそのまま Router へ渡し、結果だけを受け取る。
// web がユーザー ID を扱わないことで、判定箇所をバックエンドだけに保つ。
func (g *GraphQL) Do(ctx context.Context, token, query string, vars map[string]any, out any) error {
	b, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := g.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnauthorized {
		return ErrUnauthenticated
	}

	var body struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return fmt.Errorf("graphql: status %d: %w", res.StatusCode, err)
	}
	if len(body.Errors) > 0 {
		return classify(body.Errors)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body.Data, out)
}

func classify(errs []gqlError) error {
	var flat []gqlError
	var walk func([]gqlError)
	walk = func(es []gqlError) {
		for _, e := range es {
			flat = append(flat, e)
			walk(e.Extensions.Errors)
		}
	}
	walk(errs)

	for _, e := range flat {
		switch e.Extensions.Code {
		case "UNAUTHENTICATED":
			return ErrUnauthenticated
		case "NOT_FOUND":
			return ErrNotFound
		case "BAD_USER_INPUT":
			return &InputError{Msg: e.Message}
		}
	}
	return fmt.Errorf("graphql: %s", errs[0].Message)
}
