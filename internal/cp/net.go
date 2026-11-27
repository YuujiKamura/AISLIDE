package cp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
	"unicode/utf8"
)

const maxLine = 1 << 20

// ListenAndServe: addr（既定 127.0.0.1:9877）で待ち受ける。UTF-8 固定。ctx 終了で止まる。
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	if addr == "" {
		addr = DefaultAddr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("aislide cp listening on %s", ln.Addr())
	return s.Serve(ctx, ln)
}

// Serve: ln で接続を受ける。接続ごとに goroutine、行ごとに Dispatch（Server 内で直列化）。
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	w := bufio.NewWriter(conn)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		var resp string
		if !utf8.ValidString(line) {
			resp = ng("invalid utf-8 (send UTF-8 only)")
		} else {
			resp = s.Dispatch(ctx, line)
		}
		if _, err := w.WriteString(resp + "\n"); err != nil {
			return
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
}

// Poll: interval ごとに revision を確認し、変わっていれば再読込（人のブラウザ編集を取り込む）。
func (s *Server) Poll(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.pollOnce(ctx); err != nil {
				log.Printf("poll: %v", err)
			}
		}
	}
}

func (s *Server) pollOnce(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deck == nil || s.presID == "" {
		return nil
	}
	raw, err := s.backend.Load(ctx, s.presID)
	if err != nil {
		return err
	}
	no := s.SlideNo
	if no <= 0 {
		no = 1
	}
	d, err := s.d.parseDeck(raw, no)
	if err != nil {
		return err
	}
	if d.Rev == s.deck.Rev {
		return nil
	}
	d.Owners = map[string]string{}
	for k, v := range s.owners {
		d.Owners[k] = v
	}
	if d.PresID == "" {
		d.PresID = s.presID
	}
	s.deck, s.raw = d, raw
	log.Printf("poll: revision changed -> %s", d.Rev)
	return nil
}

// Send: CP に 1 行送り応答を返す。list は "ok" のとき空行まで読む（末尾の空行は含めない）。
func Send(addr, cmd string, timeout time.Duration) (string, error) {
	if addr == "" {
		addr = DefaultAddr
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	cmd = strings.TrimRight(cmd, "\r\n")
	if _, err := fmt.Fprintf(conn, "%s\n", cmd); err != nil {
		return "", err
	}
	r := bufio.NewReaderSize(conn, 64*1024)
	first, err := readLine(r)
	if err != nil {
		return "", err
	}
	verb := strings.ToLower(strings.Fields(cmd + " x")[0])
	if verb != "list" || !strings.HasPrefix(first, "ok") {
		return first, nil
	}
	lines := []string{first}
	for {
		l, err := readLine(r)
		if err != nil {
			return strings.Join(lines, "\n"), err
		}
		if l == "" {
			break
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n"), nil
}

func readLine(r *bufio.Reader) (string, error) {
	l, err := r.ReadString('\n')
	if err != nil && l == "" {
		return "", err
	}
	return strings.TrimRight(l, "\r\n"), nil
}
