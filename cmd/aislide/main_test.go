package main

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"
)

// 最小の CP もどき: list には複数行 + 空行、それ以外は 1 行。
func fakeCP(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					switch l := sc.Text(); {
					case strings.HasPrefix(l, "list"):
						c.Write([]byte("ok n=2\n0 base line a\n1 signs sign b 「工事中」\n\n"))
					case strings.HasPrefix(l, "move"):
						c.Write([]byte("ng read-only\n"))
					default:
						c.Write([]byte("ok echo " + l + "\n"))
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestSend(t *testing.T) {
	addr := fakeCP(t)
	var out, errb bytes.Buffer
	if code := run([]string{"send", "-addr", addr, "state"}, &out, &errb); code != 0 || out.String() != "ok echo state\n" {
		t.Fatalf("state: code=%d out=%q err=%q", code, out.String(), errb.String())
	}
	out.Reset()
	if code := run([]string{"send", "-addr", addr, "list"}, &out, &errb); code != 0 ||
		out.String() != "ok n=2\n0 base line a\n1 signs sign b 「工事中」\n" {
		t.Fatalf("list: code=%d out=%q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"send", "-addr", addr, "move", "1", "-5", "0", "agent=a"}, &out, &errb); code != 1 || out.String() != "ng read-only\n" {
		t.Fatalf("move: code=%d out=%q", code, out.String())
	}
	if code := run(nil, &out, &errb); code != 2 {
		t.Fatalf("usage code %d", code)
	}
}
