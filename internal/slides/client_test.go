package slides

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YuujiKamura/AISLIDE/internal/model"
	sl "google.golang.org/api/slides/v1"
)

// fakeAPI: ローカルの偽 Slides API。実デッキには一切書かない。
func fakeAPI(t *testing.T) (*Client, *httptest.Server, *sl.BatchUpdatePresentationRequest) {
	t.Helper()
	var got sl.BatchUpdatePresentationRequest
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("GET /v1/presentations/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"presentationId":"` + r.PathValue("id") + `","revisionId":"r1"}`))
	})
	mux.HandleFunc("POST /v1/presentations/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Go 1.22 mux は ":batchUpdate" をパス末尾に含めて渡す
		got = sl.BatchUpdatePresentationRequest{}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Error(err)
		}
		if got.WriteControl != nil && got.WriteControl.RequiredRevisionId == "old" {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"code":400,"message":"The required revision ID 'old' does not match the latest revision.","status":"FAILED_PRECONDITION"}}`))
			return
		}
		w.Write([]byte(`{"presentationId":"x","replies":[{}],"writeControl":{"requiredRevisionId":"r2"}}`))
	})
	mux.HandleFunc("GET /v1/presentations/{id}/pages/{page}/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("thumbnailProperties.thumbnailSize") != "LARGE" || r.URL.Query().Get("thumbnailProperties.mimeType") != "PNG" {
			t.Errorf("thumbnail query %v", r.URL.RawQuery)
		}
		w.Write([]byte(`{"contentUrl":"` + srv.URL + `/img.png","width":1600,"height":1131}`))
	})
	mux.HandleFunc("GET /img.png", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("\x89PNGfake")) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := newClient(context.Background(), srv.Client(), srv.Client(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	return c, srv, &got
}

func TestLoadRaw(t *testing.T) {
	c, _, _ := fakeAPI(t)
	raw, err := c.Load(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"presentationId":"abc","revisionId":"r1"}` {
		t.Fatalf("raw %s", raw)
	}
}

func TestApplyRevisionAndStale(t *testing.T) {
	c, _, got := fakeAPI(t)
	ops := []model.Op{{Kind: model.OpDelete, TargetID: "kc_a_1"}}
	rev, err := c.Apply(context.Background(), "abc", "p1", ops, "r1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rev != "r2" {
		t.Fatalf("rev %q", rev)
	}
	if got.WriteControl == nil || got.WriteControl.RequiredRevisionId != "r1" || len(got.Requests) != 1 || got.Requests[0].DeleteObject == nil {
		t.Fatalf("sent %+v", got)
	}
	_, err = c.Apply(context.Background(), "abc", "p1", ops, "old", nil)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}
	// requiredRev 空 → writeControl 無し
	if _, err := c.Apply(context.Background(), "abc", "p1", ops, "", nil); err != nil {
		t.Fatal(err)
	}
	if got.WriteControl != nil {
		t.Fatalf("writeControl should be omitted: %+v", got.WriteControl)
	}
}

func TestThumbnailDownload(t *testing.T) {
	c, _, _ := fakeAPI(t)
	p := filepath.Join(t.TempDir(), "t.png")
	if err := c.Thumbnail(context.Background(), "abc", "p1", p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "\x89PNG") {
		t.Fatalf("content %q", b)
	}
}
