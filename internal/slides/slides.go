// Package slides: Google Slides / Drive API アダプタ。Op -> batchUpdate 変換はここだけ。
package slides

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YuujiKamura/AISLIDE/internal/model"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	sl "google.golang.org/api/slides/v1"
)

// ErrStale: writeControl.requiredRevisionId が現在の revision と一致しない（他者が編集した）。
var ErrStale = errors.New("stale revision")

// KeyEnv: サービスアカウント鍵 JSON のパスを指す環境変数。
const KeyEnv = "KISEI_SA_KEY"

// DefaultKeyGlob: KeyEnv が無いときに探す鍵。
const DefaultKeyGlob = `G:\マイドライブ\_credentials\visionapi-*.json`

const defaultBase = "https://slides.googleapis.com/"

var scopes = []string{
	"https://www.googleapis.com/auth/presentations",
	"https://www.googleapis.com/auth/drive.readonly",
}

type Client struct {
	svc   *sl.Service
	authd *http.Client // OAuth 付き（API 用）
	plain *http.Client // 認証なし（thumbnail contentUrl のダウンロード用）
	base  string       // API base URL（末尾 /）
}

// tcp4Transport: Windows で IPv6 経路が ~21s ハングするのを避けるため IPv4 のみでダイヤルする。
func tcp4Transport() *http.Transport {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
		return d.DialContext(ctx, "tcp4", addr)
	}
	return t
}

// KeyPath: 鍵ファイルのパスを解決する。
func KeyPath() (string, error) {
	if p := os.Getenv(KeyEnv); p != "" {
		return p, nil
	}
	m, err := filepath.Glob(DefaultKeyGlob)
	if err != nil {
		return "", err
	}
	if len(m) == 0 {
		return "", fmt.Errorf("service account key not found: set %s or place %s", KeyEnv, DefaultKeyGlob)
	}
	return m[0], nil
}

// New: KISEI_SA_KEY または G:\マイドライブ\_credentials\visionapi-*.json のサービスアカウントで認証。
// Windows の IPv6 ハング回避のため IPv4 ダイヤラを使う（トークン取得も同じ）。
func New(ctx context.Context) (*Client, error) {
	p, err := KeyPath()
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	plain := &http.Client{Transport: tcp4Transport(), Timeout: 60 * time.Second}
	// トークンソースは長寿命なので呼出し側 ctx ではなく Background に tcp4 クライアントを載せる。
	tctx := context.WithValue(context.Background(), oauth2.HTTPClient, plain)
	creds, err := google.CredentialsFromJSONWithType(tctx, key, google.ServiceAccount, scopes...)
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}
	authd := &http.Client{
		Transport: &oauth2.Transport{Source: creds.TokenSource, Base: plain.Transport},
		Timeout:   60 * time.Second,
	}
	return newClient(ctx, authd, plain, defaultBase)
}

func newClient(ctx context.Context, authd, plain *http.Client, base string) (*Client, error) {
	svc, err := sl.NewService(ctx, option.WithHTTPClient(authd), option.WithEndpoint(base))
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc, authd: authd, plain: plain, base: base}, nil
}

// Load: presentations.get の生 JSON（testdata と同じ形）。
func (c *Client) Load(ctx context.Context, presID string) ([]byte, error) {
	u := c.base + "v1/presentations/" + url.PathEscape(presID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.authd.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := googleapi.CheckResponse(res); err != nil {
		return nil, err
	}
	return io.ReadAll(res.Body)
}

// Apply: ops を 1 回の batchUpdate で送る。requiredRev が空でなければ writeControl に付ける。新 revisionId を返す。
// revision 不一致は ErrStale（errors.Is で判定）。
func (c *Client) Apply(ctx context.Context, presID, pageID string, ops []model.Op, requiredRev string, d *model.Deck) (string, error) {
	reqs, err := ToRequests(ops, pageID, d)
	if err != nil {
		return "", err
	}
	if len(reqs) == 0 {
		return requiredRev, nil
	}
	body := &sl.BatchUpdatePresentationRequest{Requests: reqs}
	if requiredRev != "" {
		body.WriteControl = &sl.WriteControl{RequiredRevisionId: requiredRev}
	}
	res, err := c.svc.Presentations.BatchUpdate(presID, body).Context(ctx).Do()
	if err != nil {
		if isStale(err) {
			return "", fmt.Errorf("%w: %v", ErrStale, err)
		}
		return "", err
	}
	if res.WriteControl != nil {
		return res.WriteControl.RequiredRevisionId, nil
	}
	return "", nil
}

func isStale(err error) bool {
	var ge *googleapi.Error
	if !errors.As(err, &ge) {
		return false
	}
	s := strings.ToLower(ge.Message + " " + ge.Body)
	if strings.Contains(s, "failed_precondition") {
		return true
	}
	return ge.Code == http.StatusBadRequest && strings.Contains(s, "revision")
}

// Thumbnail: pages.getThumbnail(LARGE, PNG) を path に保存。
func (c *Client) Thumbnail(ctx context.Context, presID, pageID, path string) error {
	th, err := c.svc.Presentations.Pages.GetThumbnail(presID, pageID).
		ThumbnailPropertiesMimeType("PNG").
		ThumbnailPropertiesThumbnailSize("LARGE").
		Context(ctx).Do()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, th.ContentUrl, nil)
	if err != nil {
		return err
	}
	res, err := c.plain.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("thumbnail download: %s", res.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, res.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
