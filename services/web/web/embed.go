// Package web はテンプレートと静的ファイルをバイナリに埋め込む。
package web

import "embed"

//go:embed templates/*.html
var Templates embed.FS

//go:embed static
var Static embed.FS
