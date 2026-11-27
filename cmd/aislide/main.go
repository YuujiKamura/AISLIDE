// aislide: Google スライドを画面にする常駐 CP。
//
//	aislide serve [-addr 127.0.0.1:9877] [-poll 0] [-journal .kisei/journal] [-slide 1]
//	aislide send [-addr 127.0.0.1:9877] "<command>"
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/YuujiKamura/AISLIDE/internal/cp"
	"github.com/YuujiKamura/AISLIDE/internal/journal"
	"github.com/YuujiKamura/AISLIDE/internal/slides"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

const usage = `usage:
  aislide serve [-addr 127.0.0.1:9877] [-poll <sec>] [-journal <dir>] [-slide <n>]
  aislide send  [-addr 127.0.0.1:9877] [-timeout <sec>] "<command>"`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		return serve(args[1:], stderr)
	case "send":
		return send(args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
}

func serve(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", cp.DefaultAddr, "listen address")
	poll := fs.Int("poll", 0, "revision poll interval in seconds (0 = off)")
	jdir := fs.String("journal", ".kisei/journal", "undo journal directory")
	slide := fs.Int("slide", 1, "slide number (1-based)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client, err := slides.New(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "auth:", err)
		return 1
	}
	if err := os.MkdirAll(*jdir, 0o755); err != nil {
		fmt.Fprintln(stderr, "journal:", err)
		return 1
	}
	s := cp.New(client, &journal.Journal{Dir: *jdir})
	s.SlideNo = *slide
	go s.Poll(ctx, time.Duration(*poll)*time.Second)
	if err := s.ListenAndServe(ctx, *addr); err != nil {
		fmt.Fprintln(stderr, "serve:", err)
		return 1
	}
	return 0
}

func send(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", cp.DefaultAddr, "CP address")
	timeout := fs.Int("timeout", 180, "response timeout in seconds")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cmd := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if cmd == "" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	resp, err := cp.Send(*addr, cmd, time.Duration(*timeout)*time.Second)
	if resp != "" {
		fmt.Fprintln(stdout, resp)
	}
	if err != nil {
		fmt.Fprintln(stderr, "send:", err)
		return 1
	}
	if strings.HasPrefix(resp, "ng") {
		return 1
	}
	return 0
}
