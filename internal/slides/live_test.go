//go:build live

package slides

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 読み取り専用（Load + Thumbnail）。実デッキへの Apply は絶対にしない。
const livePres = "1_-5-vyTFoAcimmRW3LsZLVpENdBBPXHI5N0IkeQJ-RE"

func TestLiveLoadThumbnail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	t0 := time.Now()
	c, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.Load(ctx, livePres)
	if err != nil {
		t.Fatal(err)
	}
	tLoad := time.Since(t0)
	var p struct {
		PresentationID string `json:"presentationId"`
		RevisionID     string `json:"revisionId"`
		Slides         []struct {
			ObjectID     string            `json:"objectId"`
			PageElements []json.RawMessage `json:"pageElements"`
		} `json:"slides"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.PresentationID != livePres || len(p.Slides) == 0 {
		t.Fatalf("unexpected presentation: id=%s slides=%d", p.PresentationID, len(p.Slides))
	}
	t.Logf("load: %v bytes=%d rev=%s page=%s elements=%d", tLoad, len(raw), p.RevisionID, p.Slides[0].ObjectID, len(p.Slides[0].PageElements))

	t1 := time.Now()
	path := filepath.Join(os.TempDir(), "aislide_live_thumb.png")
	if err := c.Thumbnail(ctx, livePres, p.Slides[0].ObjectID, path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() < 1000 {
		t.Fatalf("thumbnail %v %v", st, err)
	}
	t.Logf("thumbnail: %v -> %s (%d bytes)", time.Since(t1), path, st.Size())
}
