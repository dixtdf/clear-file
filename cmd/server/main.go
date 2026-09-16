// Command server runs clear-file: a REST + SSE API plus the
// embedded Vue 3 web UI.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"file-cleaner/internal/api"
	"file-cleaner/internal/config"
	"file-cleaner/internal/filesystem"
	"file-cleaner/internal/task"
	"file-cleaner/internal/version"
	"file-cleaner/web"
)

func main() {
	cfg := config.Load()

	addr := flag.String("addr", cfg.Addr, "HTTP listen address, e.g. :6888")
	root := flag.String("root", cfg.Root, "only directory tree the service may access")
	verbose := flag.Bool("verbose", false, "log every request")
	flag.Parse()

	cfg.Addr = *addr
	cfg.Root = filesystem.CleanRoot(*root)

	if fi, err := os.Stat(cfg.Root); err != nil || !fi.IsDir() {
		log.Printf("warn: root %q is not a readable directory (mount it with -v /mnt:/mnt)", cfg.Root)
	}

	manager := task.NewManager(50)
	srv := api.New(cfg, manager)

	handler := srv.Handler(web.Handler())
	if *verbose {
		handler = logRequests(handler)
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Scans are long lived but they run as tasks; requests stay short.
		WriteTimeout: 0,
	}

	go func() {
		log.Printf("file-cleaner %s listening on %s (root=%s, trash=%v)", version.String(), cfg.Addr, cfg.Root, cfg.TrashMode)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
