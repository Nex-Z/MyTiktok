package main

import (
	"context"
	"log"
	"mytiktok/internal/app"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		r, e := client.Get("http://127.0.0.1:8080/api/health")
		if e != nil {
			os.Exit(1)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	a, err := app.New(app.Config{DataDir: env("DATA_DIR", "data"), WebDir: env("WEB_DIR", "web/dist"), MediaRoot: env("MEDIA_ROOT", "/media")})
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()
	a.Start()
	s := &http.Server{Addr: env("LISTEN_ADDR", ":8080"), Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("MyTiktok listening on %s", s.Addr)
		if e := s.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			serverErrors <- e
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case e := <-serverErrors:
		log.Print(e)
		return
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.Shutdown(shutdown)
}
