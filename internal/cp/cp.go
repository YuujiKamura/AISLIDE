// Package cp: AICAD CP 互換の 1 行プロトコル TCP サーバとコマンド dispatch。
package cp

import "context"

// Backend: slides.Client を抽象化（テストで fake を差す）。
type Backend interface {
	Load(ctx context.Context, presID string) ([]byte, error)
	Apply(ctx context.Context, presID, pageID string, ops any, requiredRev string, deck any) (string, error)
	Thumbnail(ctx context.Context, presID, pageID, path string) error
}

type Server struct{}

// Dispatch: 1 行コマンドを処理し 1 行（list のみ複数行）の応答を返す。
func (s *Server) Dispatch(ctx context.Context, line string) string { panic("TODO") }

// ListenAndServe: 127.0.0.1:9877。UTF-8 固定。
func (s *Server) ListenAndServe(ctx context.Context, addr string) error { panic("TODO") }
