package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ilodezis/aptekamos-mcp/internal/aptekamos"
	internalmcp "github.com/ilodezis/aptekamos-mcp/internal/mcp"
	"github.com/ilodezis/aptekamos-mcp/internal/oauth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sign-token" {
		if err := signTokenCmd(os.Args[2:], os.Getenv, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "sign-token:", err)
			os.Exit(1)
		}
		return
	}

	httpAddr := flag.String("http", "", "HTTP listen address (e.g. :8130); defaults to ADDR, then PORT")
	stdio := flag.Bool("stdio", false, "serve over stdio (the default when no HTTP address is set)")
	flag.Parse()

	addr := *httpAddr
	if addr == "" {
		addr = os.Getenv("ADDR")
	}
	if addr == "" {
		if port := os.Getenv("PORT"); port != "" {
			addr = ":" + strings.TrimPrefix(port, ":")
		}
	}

	srv := internalmcp.NewServer(aptekamos.NewService(aptekamos.NewClient()))
	if *stdio || addr == "" {
		runStdio(srv)
		return
	}
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	runHTTP(srv, addr)
}

func runStdio(srv *internalmcp.Server) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := srv.MCPServer().Run(ctx, &mcp.StdioTransport{}); err != nil && err != context.Canceled {
		log.Fatalf("stdio server: %v", err)
	}
}

func runHTTP(srv *internalmcp.Server, addr string) {
	var redirectURIs []string
	for _, u := range strings.Split(os.Getenv("REDIRECT_URIS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			redirectURIs = append(redirectURIs, u)
		}
	}
	gate, err := oauth.NewServer(oauth.Config{
		Password:     os.Getenv("PASSWORD"),
		TokenSecret:  os.Getenv("TOKEN_SECRET"),
		ClientID:     os.Getenv("CLIENT_ID"),
		ClientSecret: os.Getenv("CLIENT_SECRET"),
		RedirectURIs: redirectURIs,
	})
	if err != nil {
		log.Fatalf("OAuth configuration: %v", err)
	}

	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv.MCPServer() },
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: true})

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", gate.HandleMetadata)
	mux.HandleFunc("/oauth/authorize", gate.HandleAuthorize)
	mux.HandleFunc("/oauth/token", gate.HandleToken)
	mux.Handle("/mcp", mcpHandler)
	mux.Handle("/mcp/", mcpHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	server := &http.Server{Addr: addr, Handler: gate.BearerGate(mux), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("aptekamos-mcp listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server: %v", err)
	}
}
