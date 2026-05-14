package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"gtm-mcp-server/auth"
	"gtm-mcp-server/config"
	"gtm-mcp-server/gtm"
	"gtm-mcp-server/middleware"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

//go:embed llms.txt
var llmsTxt string

const (
	serverName    = "gtm-mcp-server"
	serverVersion = "1.6.0"
)

func main() {
	// Set up structured logging to stderr (stdout is reserved for MCP in stdio mode)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Adjust log level
	if cfg.LogLevel == "debug" {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
		slog.SetDefault(logger)
	}

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	// Add logging middleware
	server.AddReceivingMiddleware(middleware.NewLoggingMiddleware(logger))

	// Register tools
	registerTools(server)

	// Create HTTP handler for MCP
	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, nil)

	// Set up HTTP routes
	mux := http.NewServeMux()

	// Health check endpoint (no auth required)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "healthy",
			"service": serverName,
			"version": serverVersion,
		})
	})

	// LLM context endpoint (no auth required)
	mux.HandleFunc("GET /llms.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(llmsTxt))
	})

	// URL resolver for dynamic base URL resolution in Docker-to-Docker contexts.
	// Only resolves dynamically for hosts in the allowlist; falls back to cfg.BaseURL.
	var urlResolver *auth.URLResolver
	if len(cfg.AllowedHosts) > 0 {
		urlResolver = auth.NewURLResolver(cfg.BaseURL, cfg.AllowedHosts)
		logger.Info("dynamic URL resolution enabled", "allowed_hosts", cfg.AllowedHosts)
	}

	// OAuth metadata endpoints (always served, no auth required)
	// RFC 9728: Protected Resource Metadata - tells clients where to find the authorization server
	mux.HandleFunc("GET /.well-known/oauth-protected-resource",
		auth.ProtectedResourceMetadataHandler(cfg.BaseURL, cfg.BaseURL+"/mcp", urlResolver))

	// RFC 8414: Authorization Server Metadata - tells clients about OAuth endpoints
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", auth.MetadataHandler(cfg.BaseURL, urlResolver))

	// Service account S2S mode (runs alongside OAuth when both are configured)
	var saTokenSource oauth2.TokenSource
	if cfg.ServiceAccountAPIKey != "" {
		var saErr error
		saTokenSource, saErr = auth.NewServiceAccountTokenSource(context.Background(), cfg.ServiceAccountKeyJSON)
		if saErr != nil {
			logger.Error("s2s_mode_failed",
				"error", saErr,
				"hint", "set GOOGLE_SERVICE_ACCOUNT_KEY_JSON or deploy on GCP for Workload Identity",
			)
			os.Exit(1)
		}
		credSource := "workload_identity"
		if cfg.ServiceAccountKeyJSON != "" {
			credSource = "key_json"
		}
		logger.Info("s2s_mode_enabled", "credential_source", credSource)
	}

	// Check if OAuth is configured
	var authServer *auth.Server
	var tokenStore auth.TokenStore
	oauthConfigured := cfg.ValidateAuth() == nil

	// Rate limiters for public endpoints
	oauthLimiter := middleware.NewRateLimiter(10, 20, cfg.TrustProxy)   // 10 req/s, burst 20
	registerLimiter := middleware.NewRateLimiter(2, 5, cfg.TrustProxy)   // 2 req/s, burst 5

	if oauthConfigured {
		// Set up OAuth
		tokenStore = auth.NewMemoryTokenStore()
		googleProvider := auth.NewGoogleProvider(
			cfg.GoogleClientID,
			cfg.GoogleClientSecret,
			cfg.BaseURL+"/oauth/callback",
		)
		authServer = auth.NewServer(cfg.BaseURL, googleProvider, tokenStore, logger, cfg.AccessTokenTTL)

		// OAuth endpoints with rate limiting and body size limits
		mux.HandleFunc("GET /authorize", oauthLimiter.MiddlewareFunc(authServer.AuthorizeHandler))
		mux.HandleFunc("GET /oauth/callback", oauthLimiter.MiddlewareFunc(authServer.CallbackHandler))
		mux.HandleFunc("POST /oauth/token", oauthLimiter.MiddlewareFunc(middleware.MaxBytesMiddleware(1<<20, authServer.TokenHandler)))
		mux.HandleFunc("POST /register", registerLimiter.MiddlewareFunc(middleware.MaxBytesMiddleware(1<<20, authServer.RegistrationHandler)))

		// MCP endpoint with REQUIRED auth middleware and body size limit
		// Returns 401 if no valid Bearer token - triggers Claude's OAuth flow
		authMiddleware := auth.Middleware(tokenStore, googleProvider, logger, cfg.BaseURL, cfg.AccessTokenTTL, urlResolver, saTokenSource, cfg.ServiceAccountAPIKey)
		mux.Handle("/", authMiddleware(maxBytesHandler(5<<20, mcpHandler)))

		logger.Info("OAuth configured",
			"authorize_endpoint", cfg.BaseURL+"/authorize",
			"token_endpoint", cfg.BaseURL+"/token",
			"callback_endpoint", cfg.BaseURL+"/oauth/callback",
			"register_endpoint", cfg.BaseURL+"/register",
			"protected_resource_metadata", cfg.BaseURL+"/.well-known/oauth-protected-resource",
			"authorization_server_metadata", cfg.BaseURL+"/.well-known/oauth-authorization-server",
		)
	} else if saTokenSource != nil {
		// S2S-only PKCE mode: no Google OAuth app required.
		// claude.ai performs PKCE; we issue cfg.ServiceAccountAPIKey as the access token.
		var pkceMu sync.Mutex
		type pkceEntry struct {
			codeChallenge string
			expiresAt     time.Time
		}
		pkceCodes := map[string]pkceEntry{}

		// /authorize: accept code_challenge, redirect with a one-time code
		mux.HandleFunc("GET /authorize", oauthLimiter.MiddlewareFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			codeChallenge := q.Get("code_challenge")
			redirectURI := q.Get("redirect_uri")
			state := q.Get("state")

			if codeChallenge == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request", "error_description": "code_challenge required"})
				return
			}

			h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
			code := base64.RawURLEncoding.EncodeToString(h[:])[:32]

			pkceMu.Lock()
			pkceCodes[code] = pkceEntry{codeChallenge: codeChallenge, expiresAt: time.Now().Add(5 * time.Minute)}
			pkceMu.Unlock()

			redir, parseErr := url.Parse(redirectURI)
			if parseErr != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request"})
				return
			}
			qr := redir.Query()
			qr.Set("code", code)
			if state != "" {
				qr.Set("state", state)
			}
			redir.RawQuery = qr.Encode()
			http.Redirect(w, r, redir.String(), http.StatusFound)
		}))

		// /token: client_credentials or PKCE authorization_code exchange; returns SERVICE_ACCOUNT_API_KEY as bearer
		mux.HandleFunc("POST /oauth/token", oauthLimiter.MiddlewareFunc(middleware.MaxBytesMiddleware(1<<20, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if parseErr := r.ParseForm(); parseErr != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request"})
				return
			}
			grantType := r.FormValue("grant_type")

			// client_credentials grant: validate client_id + client_secret, return bearer token directly
			if grantType == "client_credentials" {
				clientID := r.FormValue("client_id")
				clientSecret := r.FormValue("client_secret")
				if clientID == "" {
					// Also accept Basic auth header
					clientID, clientSecret, _ = r.BasicAuth()
				}
				if cfg.OAuthClientID == "" || cfg.OAuthClientSecret == "" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					json.NewEncoder(w).Encode(map[string]string{"error": "server_error", "error_description": "client_credentials not configured"})
					return
				}
				if clientID != cfg.OAuthClientID || clientSecret != cfg.OAuthClientSecret {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token": cfg.ServiceAccountAPIKey,
					"token_type":   "Bearer",
					"expires_in":   2592000,
				})
				return
			}

			// authorization_code grant: PKCE exchange
			code := r.FormValue("code")
			verifier := r.FormValue("code_verifier")

			pkceMu.Lock()
			entry, ok := pkceCodes[code]
			if ok {
				delete(pkceCodes, code)
			}
			pkceMu.Unlock()

			if !ok || time.Now().After(entry.expiresAt) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}

			// Verify PKCE S256: BASE64URL(SHA256(verifier)) must equal stored code_challenge
			h := sha256.Sum256([]byte(verifier))
			challenge := base64.RawURLEncoding.EncodeToString(h[:])
			if challenge != entry.codeChallenge {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": cfg.ServiceAccountAPIKey,
				"token_type":   "Bearer",
				"expires_in":   2592000,
			})
		}))))

		// /register: minimal DCR (RFC 7591); echo back a static client_id
		mux.HandleFunc("POST /register", registerLimiter.MiddlewareFunc(middleware.MaxBytesMiddleware(1<<20, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{
				"client_id":     "claude-pathfinder",
				"client_secret": "",
			})
		}))))

		// MCP endpoint: Bearer middleware validates against SERVICE_ACCOUNT_API_KEY
		s2sStore := auth.NewMemoryTokenStore()
		authMiddleware := auth.Middleware(s2sStore, nil, logger, cfg.BaseURL, cfg.AccessTokenTTL, urlResolver, saTokenSource, cfg.ServiceAccountAPIKey)
		mux.Handle("/", authMiddleware(maxBytesHandler(5<<20, mcpHandler)))

		logger.Info("s2s_pkce_mode_active",
			"authorize_endpoint", cfg.BaseURL+"/authorize",
			"token_endpoint", cfg.BaseURL+"/token",
			"register_endpoint", cfg.BaseURL+"/register",
		)
	} else {
		logger.Warn("OAuth not configured, running without authentication", "error", cfg.ValidateAuth())

		// Register OAuth endpoints that return proper errors
		oauthNotConfiguredHandler := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "server_error",
				"error_description": "OAuth is not configured on this server.",
			})
		}
		mux.HandleFunc("GET /authorize", oauthLimiter.MiddlewareFunc(oauthNotConfiguredHandler))
		mux.HandleFunc("GET /oauth/callback", oauthLimiter.MiddlewareFunc(oauthNotConfiguredHandler))
		mux.HandleFunc("POST /oauth/token", oauthLimiter.MiddlewareFunc(oauthNotConfiguredHandler))
		mux.HandleFunc("POST /register", registerLimiter.MiddlewareFunc(oauthNotConfiguredHandler))

		// MCP endpoint without auth (still apply body size limit)
		mux.Handle("/", maxBytesHandler(5<<20, mcpHandler))
	}

	// Create HTTP server
	addr := fmt.Sprintf(":%d", cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // Disabled for SSE streams
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start server
	go func() {
		logger.Info("starting GTM MCP server",
			"port", cfg.Port,
			"base_url", cfg.BaseURL,
		)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	logger.Info("shutting down server")

	// Give outstanding requests 10 seconds to complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}

	logger.Info("server stopped")
}

// registerTools adds MCP tools to the server.
func registerTools(server *mcp.Server) {
	registerUtilityTools(server)
	gtm.RegisterTools(server)
}

// maxBytesHandler wraps an http.Handler with a request body size limit.
func maxBytesHandler(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// registerUtilityTools adds ping and auth_status tools.
func registerUtilityTools(server *mcp.Server) {
	// Ping tool for testing connectivity
	type PingInput struct {
		Message string `json:"message,omitempty" jsonschema:"Optional message to echo back"`
	}
	type PingOutput struct {
		Reply     string `json:"reply"`
		Timestamp string `json:"timestamp"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ping",
		Description: "Test connectivity to the GTM MCP server",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input PingInput) (*mcp.CallToolResult, PingOutput, error) {
		reply := "pong"
		if input.Message != "" {
			reply = fmt.Sprintf("pong: %s", input.Message)
		}
		return nil, PingOutput{Reply: reply, Timestamp: time.Now().UTC().Format(time.RFC3339)}, nil
	})

	// Auth status tool
	type AuthStatusInput struct{}
	type AuthStatusOutput struct {
		Authenticated bool   `json:"authenticated"`
		Message       string `json:"message"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "auth_status",
		Description: "Check authentication status with Google Tag Manager",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input AuthStatusInput) (*mcp.CallToolResult, AuthStatusOutput, error) {
		tokenInfo := auth.GetTokenInfo(ctx)
		output := AuthStatusOutput{Authenticated: tokenInfo != nil}
		if tokenInfo != nil {
			output.Message = "You are authenticated and can access GTM data"
		} else {
			output.Message = "Not authenticated. GTM tools will require authentication."
		}
		return nil, output, nil
	})
}
