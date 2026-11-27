// Package slides: Google Slides / Drive API アダプタ。Op -> batchUpdate 変換はここだけ。
package slides

import (
	"context"

	"github.com/YuujiKamura/AISLIDE/internal/model"
	sl "google.golang.org/api/slides/v1"
)

type Client struct{}

// New: KISEI_SA_KEY または G:\マイドライブ\_credentials\visionapi-*.json のサービスアカウントで認証。
// Windows の IPv6 ハング回避のため IPv4 優先ダイヤラを使う。
func New(ctx context.Context) (*Client, error) { panic("TODO") }

// Load: presentations.get の生 JSON。
func (c *Client) Load(ctx context.Context, presID string) ([]byte, error) { panic("TODO") }

// Apply: ops を 1 回の batchUpdate で送る。requiredRev が空でなければ writeControl に付ける。新 revisionId を返す。
func (c *Client) Apply(ctx context.Context, presID, pageID string, ops []model.Op, requiredRev string, d *model.Deck) (string, error) {
	panic("TODO")
}

// Thumbnail: pages.getThumbnail(LARGE, PNG) を path に保存。
func (c *Client) Thumbnail(ctx context.Context, presID, pageID, path string) error { panic("TODO") }

// ToRequests: 純関数。d は Transform の scale 算出（SizeEMU）に使う。
func ToRequests(ops []model.Op, pageID string, d *model.Deck) ([]*sl.Request, error) { panic("TODO") }
