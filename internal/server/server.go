package server

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	svcrypto "github.com/qoder/device-mgmt/internal/crypto"
	"github.com/qoder/device-mgmt/internal/protocol"
	"github.com/qoder/device-mgmt/internal/server/auth"
	"github.com/qoder/device-mgmt/internal/server/database"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// Server is the main server struct
type Server struct {
	config          *Config
	db              *database.DB
	hub             *Hub
	jwtMgr          *auth.JWTManager
	pskMgr          *auth.PSKManager
	portPool        *PortPool
	logger          *zap.Logger
	httpServer      *http.Server
	wsUpgrader      websocket.Upgrader
	tunnelListeners sync.Map // tunnelID -> net.Listener
	tunnelTimers    sync.Map // tunnelID -> *time.Timer
}

// New creates a new Server instance
func New(cfg *Config, db *database.DB, logger *zap.Logger) *Server {
	jwtMgr := auth.NewJWTManager(cfg.Auth.JWTSecret, cfg.Auth.JWTExpiry)
	pskMgr := auth.NewPSKManager(cfg.Auth.PSK)
	hub := NewHub(logger)

	portPool, err := NewPortPool(cfg.Tunnel.BindRange)
	if err != nil {
		logger.Warn("failed to create port pool, using default", zap.Error(err))
		portPool, _ = NewPortPool("127.0.0.1:10000-20000")
	}

	s := &Server{
		config:   cfg,
		db:       db,
		hub:      hub,
		jwtMgr:   jwtMgr,
		pskMgr:   pskMgr,
		portPool: portPool,
		logger:   logger,
		wsUpgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true // non-browser clients (agent) have no Origin
				}
				host := r.Host
				// Allow same-origin WebSocket connections
				return strings.HasPrefix(origin, "http://"+host) || strings.HasPrefix(origin, "https://"+host) ||
					strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "https://localhost")
			},
		},
	}

	hub.OnAgentConnect = s.onAgentConnect
	hub.OnAgentDisconnect = s.onAgentDisconnect

	return s
}

// Start starts the server
func (s *Server) Start(ctx context.Context) error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	router.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			origin = "*"
		} else {
			// Only allow same-origin or localhost
			host := c.Request.Host
			if !strings.HasPrefix(origin, "http://"+host) &&
				!strings.HasPrefix(origin, "https://"+host) &&
				!strings.HasPrefix(origin, "http://localhost") &&
				!strings.HasPrefix(origin, "https://localhost") {
				origin = ""
			}
		}
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Vary", "Origin")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	s.registerRoutes(router)

	// Restore auto-close timers for active tunnels from previous session
	s.restoreTunnelTimers()

	// Create a custom error log that suppresses noisy TLS handshake errors
	// These are benign warnings from clients probing the HTTPS port with HTTP
	errorLog := log.New(io.Discard, "", 0)

	s.httpServer = &http.Server{
		Addr:     s.config.Server.Addr,
		Handler:  router,
		ErrorLog: errorLog, // Suppress TLS handshake error spam
	}

	s.logger.Info("server starting", zap.String("addr", s.config.Server.Addr))

	var err error
	if s.config.Server.TLS.Cert != "" && s.config.Server.TLS.Key != "" {
		err = s.httpServer.ListenAndServeTLS(s.config.Server.TLS.Cert, s.config.Server.TLS.Key)
	} else {
		err = s.httpServer.ListenAndServe()
	}

	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server listen: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("server shutting down")
	return s.httpServer.Shutdown(ctx)
}

// registerRoutes sets up all HTTP routes with handlers in this package
func (s *Server) registerRoutes(router *gin.Engine) {
	router.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	// Agent WebSocket is NOT protected by IP whitelist — devices connect independently
	router.GET("/ws/agent", s.handleAgentWebSocket)

	// Web access control middleware: applies to API routes and frontend only
	webIPGuard := auth.IPWhitelistMiddleware(func() auth.WebAccessControlConfig {
		return auth.WebAccessControlConfig{
			Enabled:    s.config.WebAccessControl.Enabled,
			AllowedIPs: s.config.WebAccessControl.AllowedIPs,
		}
	})

	// Browser WebSocket is behind IP whitelist (it's a web client)
	router.GET("/ws/browser", webIPGuard, s.handleBrowserWebSocket)

	apiGroup := router.Group("/api/v1")
	apiGroup.Use(webIPGuard)

	// Auth
	authGroup := apiGroup.Group("/auth")
	{
		authGroup.POST("/login", s.handleLogin)
		authGroup.POST("/refresh", auth.JWTMiddleware(s.jwtMgr), s.handleRefresh)
	}

	// Protected
	p := apiGroup.Group("")
	p.Use(auth.JWTMiddleware(s.jwtMgr))
	{
		p.GET("/devices", s.handleListDevices)
		p.GET("/devices/:id", s.handleGetDevice)
		p.PUT("/devices/:id/notes", s.handleUpdateDeviceNotes)
		p.DELETE("/devices/:id", auth.RequirePermission(auth.PermDeleteDevices), s.handleDeleteDevice)
		p.POST("/devices/:id/shell", auth.RequirePermission(auth.PermShell), s.handleShellStart)
		p.GET("/devices/:id/files", auth.RequirePermission(auth.PermFileBrowse), s.handleFileBrowse)
		p.GET("/devices/:id/files/content", auth.RequirePermission(auth.PermFileBrowse), s.handleFileRead)
		p.PUT("/devices/:id/files/content", auth.RequirePermission(auth.PermFileWrite), s.handleFileWrite)
		p.POST("/devices/:id/files/upload", auth.RequirePermission(auth.PermFileWrite), s.handleFileUpload)
		p.POST("/devices/:id/files/upload/chunk", auth.RequirePermission(auth.PermFileWrite), s.handleFileUploadChunk)
		p.POST("/devices/:id/files/upload/done", auth.RequirePermission(auth.PermFileWrite), s.handleFileUploadDone)
		p.GET("/devices/:id/files/download", auth.RequirePermission(auth.PermFileBrowse), s.handleFileDownload)
		p.GET("/devices/:id/files/download/dir", auth.RequirePermission(auth.PermFileBrowse), s.handleFileDownloadDir)
		p.POST("/devices/:id/files/create", auth.RequirePermission(auth.PermFileWrite), s.handleFileCreate)
		p.POST("/devices/:id/files/mkdir", auth.RequirePermission(auth.PermFileWrite), s.handleFileMkdir)
		p.DELETE("/devices/:id/files", auth.RequirePermission(auth.PermFileWrite), s.handleFileDelete)
		p.POST("/devices/:id/files/move", auth.RequirePermission(auth.PermFileWrite), s.handleFileMove)
		p.POST("/devices/:id/files/chmod", auth.RequirePermission(auth.PermFileWrite), s.handleFileChmod)
		p.GET("/devices/:id/files/search", auth.RequirePermission(auth.PermFileBrowse), s.handleFileSearch)
		p.GET("/devices/:id/processes", s.handleProcessList)
		p.GET("/devices/:id/system", s.handleSystemInfo)
		p.POST("/devices/:id/processes/:pid/kill", auth.RequirePermission(auth.PermProcessKill), s.handleProcessKill)
		p.GET("/tunnels", s.handleListTunnels)
		p.POST("/tunnels", auth.RequirePermission(auth.PermTunnelCreate), s.handleCreateTunnel)
		p.DELETE("/tunnels/:id", auth.RequirePermission(auth.PermTunnelCreate), s.handleDeleteTunnel)
		p.PUT("/tunnels/:id/renew", auth.RequirePermission(auth.PermTunnelCreate), s.handleRenewTunnel)
		p.DELETE("/tunnels/:id/permanent", auth.RequirePermission(auth.PermTunnelCreate), s.handleHardDeleteTunnel)
		p.POST("/tunnels/socks5", auth.RequirePermission(auth.PermTunnelCreate), s.handleCreateSocks5)
		p.POST("/agent/build", auth.RequirePermission(auth.PermManageUsers), s.handleAgentBuild)
		p.GET("/agent/download", auth.RequirePermission(auth.PermManageUsers), s.handleAgentDownload)
		users := p.Group("/users")
		users.Use(auth.RequirePermission(auth.PermManageUsers))
		{
			users.GET("", s.handleListUsers)
			users.POST("", s.handleCreateUser)
			users.PUT("/:id", s.handleUpdateUser)
			users.DELETE("/:id", s.handleDeleteUser)
		}
		p.GET("/audit-logs", auth.RequirePermission(auth.PermViewAuditLogs), s.handleAuditLogs)
		p.GET("/settings", s.handleGetSettings)
		p.PUT("/settings", auth.RequirePermission(auth.PermManageUsers), s.handleUpdateSettings)
	}

	// Serve frontend static files (SPA)
	s.serveFrontend(router)
}

// ---- Auth handlers ----

func (s *Server) handleLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Error("login bind error", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	user, err := s.db.GetUserByUsername(c.Request.Context(), req.Username)
	if err != nil {
		s.logger.Error("login db error", zap.Error(err))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if user == nil {
		s.logger.Warn("login user not found", zap.String("username", req.Username))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if !user.Enabled {
		s.logger.Warn("login user disabled", zap.String("username", req.Username))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		s.logger.Warn("login password mismatch", zap.String("username", req.Username), zap.Error(err))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token, expiresAt, err := s.jwtMgr.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": expiresAt, "user": gin.H{
		"id": user.ID, "username": user.Username, "display_name": user.DisplayName, "role": user.Role, "enabled": user.Enabled,
	}})
}

func (s *Server) handleRefresh(c *gin.Context) {
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")
	role, _ := c.Get("role")
	token, expiresAt, err := s.jwtMgr.GenerateToken(userID.(string), username.(string), role.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expires_at": expiresAt})
}

// ---- Device handlers ----

func (s *Server) handleListDevices(c *gin.Context) {
	devices, err := s.db.ListDevices(c.Request.Context(), database.DeviceFilter{Status: c.Query("status"), Keyword: c.Query("keyword")})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list devices"})
		return
	}
	if devices == nil {
		devices = []*database.Device{}
	}
	c.JSON(http.StatusOK, devices)
}

func (s *Server) handleGetDevice(c *gin.Context) {
	device, err := s.db.GetDevice(c.Request.Context(), c.Param("id"))
	if err != nil || device == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	c.JSON(http.StatusOK, device)
}

func (s *Server) handleDeleteDevice(c *gin.Context) {
	if err := s.db.DeleteDevice(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete device"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) handleUpdateDeviceNotes(c *gin.Context) {
	var req struct {
		Notes string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if err := s.db.UpdateDeviceNotes(c.Request.Context(), c.Param("id"), req.Notes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update notes"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"notes": req.Notes})
}

// ---- Shell handler ----

func (s *Server) handleShellStart(c *gin.Context) {
	deviceID := c.Param("id")
	if !s.hub.IsDeviceOnline(deviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		Cols      uint16 `json:"cols"`
		Rows      uint16 `json:"rows"`
		ShellPath string `json:"shell_path"`
	}
	c.ShouldBindJSON(&req)
	if req.Cols == 0 {
		req.Cols = 80
	}
	if req.Rows == 0 {
		req.Rows = 24
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	env, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelShell, protocol.TypeRequest, protocol.ActionShellStart,
		protocol.ShellStartPayload{SessionID: sessionID, Cols: req.Cols, Rows: req.Rows, ShellPath: req.ShellPath})
	resp, err := s.hub.RequestToDevice(deviceID, env, 10*time.Second)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if resp.Error != nil {
		c.JSON(resp.Error.Code, gin.H{"error": resp.Error.Message})
		return
	}
	userID, _ := c.Get("user_id")
	s.db.CreateAuditLog(c.Request.Context(), &database.AuditEntry{UserID: userID.(string), DeviceID: deviceID, Action: protocol.ActionShellStart, Resource: sessionID, SourceIP: c.ClientIP()})
	c.JSON(http.StatusOK, gin.H{"session_id": sessionID})
}

// ---- File handlers ----

func (s *Server) handleFileBrowse(c *gin.Context) {
	s.proxyToDevice(c, protocol.ChannelFile, protocol.ActionFileBrowse, protocol.FileBrowsePayload{Path: c.DefaultQuery("path", "/")})
}

func (s *Server) handleFileRead(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}
	s.proxyToDevice(c, protocol.ChannelFile, protocol.ActionFileRead, protocol.FileReadPayload{Path: path})
}

func (s *Server) handleFileWrite(c *gin.Context) {
	var req protocol.FileWritePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp := s.proxyToDeviceRaw(c, protocol.ChannelFile, protocol.ActionFileWrite, req)
	if resp != nil && resp.Error == nil {
		userID, _ := c.Get("user_id")
		s.db.CreateAuditLog(c.Request.Context(), &database.AuditEntry{UserID: userID.(string), DeviceID: c.Param("id"), Action: protocol.ActionFileWrite, Resource: req.Path, SourceIP: c.ClientIP()})
	}
}

func (s *Server) handleFileUpload(c *gin.Context) {
	var req struct {
		Path       string `json:"path" binding:"required"`
		Size       int64  `json:"size"`
		TransferID string `json:"transfer_id,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	transferID := req.TransferID
	if transferID == "" {
		transferID = protocol.GenerateID()
	}
	deviceID := c.Param("id")
	if !s.hub.IsDeviceOnline(deviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}
	env, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelFile, protocol.TypeRequest, protocol.ActionFileUploadStart,
		protocol.FileUploadStartPayload{TransferID: transferID, Path: req.Path, Size: req.Size})
	resp, err := s.hub.RequestToDevice(deviceID, env, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if resp.Error != nil {
		c.JSON(resp.Error.Code, gin.H{"error": resp.Error.Message})
		return
	}
	// Return transfer_id and chunk_size in response
	var result struct {
		TransferID string `json:"transfer_id"`
		ChunkSize  int    `json:"chunk_size"`
	}
	resp.DecodePayload(&result)
	if result.TransferID == "" {
		result.TransferID = transferID
	}
	if result.ChunkSize == 0 {
		result.ChunkSize = 256 * 1024
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleFileUploadChunk(c *gin.Context) {
	var req protocol.FileUploadChunkPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.proxyToDeviceRaw(c, protocol.ChannelFile, protocol.ActionFileUploadChunk, req)
}

func (s *Server) handleFileUploadDone(c *gin.Context) {
	var req protocol.FileUploadDonePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.proxyToDeviceRaw(c, protocol.ChannelFile, protocol.ActionFileUploadDone, req)
}

func (s *Server) handleFileDownload(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}
	deviceID := c.Param("id")
	if !s.hub.IsDeviceOnline(deviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}

	transferID := protocol.GenerateID()

	// Register download stream BEFORE sending request to agent.
	// Agent starts sending chunks in a goroutine before the response returns,
	// so we must be ready to receive them to avoid losing early chunks.
	ch := make(chan downloadChunk, 64)
	s.hub.RegisterDownloadStream(transferID, ch)

	env, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelFile, protocol.TypeRequest, protocol.ActionFileDownloadStart,
		protocol.FileDownloadStartPayload{TransferID: transferID, Path: path})

	resp, err := s.hub.RequestToDevice(deviceID, env, 30*time.Second)
	if err != nil {
		s.hub.UnregisterDownloadStream(transferID)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if resp.Error != nil {
		s.hub.UnregisterDownloadStream(transferID)
		c.JSON(resp.Error.Code, gin.H{"error": resp.Error.Message})
		return
	}

	// Extract file name from path
	fileName := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		fileName = path[idx+1:]
	}

	s.streamDownloadToHTTP(c, ch, transferID, fileName)
}

func (s *Server) handleFileDownloadDir(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}
	deviceID := c.Param("id")
	if !s.hub.IsDeviceOnline(deviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}

	transferID := protocol.GenerateID()

	// Register download stream BEFORE sending request to agent
	ch := make(chan downloadChunk, 64)
	s.hub.RegisterDownloadStream(transferID, ch)

	env, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelFile, protocol.TypeRequest, protocol.ActionFileDownloadDir,
		protocol.FileDownloadDirPayload{TransferID: transferID, Path: path})

	resp, err := s.hub.RequestToDevice(deviceID, env, 60*time.Second)
	if err != nil {
		s.hub.UnregisterDownloadStream(transferID)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if resp.Error != nil {
		s.hub.UnregisterDownloadStream(transferID)
		c.JSON(resp.Error.Code, gin.H{"error": resp.Error.Message})
		return
	}

	var result protocol.FileDownloadDirStartResult
	resp.DecodePayload(&result)

	zipName := result.Name
	if zipName == "" {
		dirName := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			dirName = path[idx+1:]
		}
		if dirName == "" {
			dirName = "download"
		}
		zipName = dirName + ".zip"
	}

	s.streamDownloadToHTTP(c, ch, transferID, zipName)
}

// streamDownloadToHTTP bridges a download transfer to an HTTP response stream.
// The channel must be registered in Hub BEFORE sending the request to the agent,
// to avoid losing early chunks due to the race between agent goroutine and response.
func (s *Server) streamDownloadToHTTP(c *gin.Context, ch chan downloadChunk, transferID string, fileName string) {
	defer s.hub.UnregisterDownloadStream(transferID)

	// Sanitize fileName to prevent HTTP header injection (CRLF / quote escaping)
	safeName := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '\r' || r == '\n' {
			return '_'
		}
		return r
	}, fileName)

	// Set response headers for file download
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeName))
	c.Header("Cache-Control", "no-cache")
	c.Status(http.StatusOK)

	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if chunk.Action == protocol.ActionFileDownloadDone {
				return
			}
			if chunk.Data != "" {
				decoded, err := base64.StdEncoding.DecodeString(chunk.Data)
				if err != nil {
					s.logger.Error("failed to decode download chunk", zap.Error(err))
					return
				}
				if _, err := c.Writer.Write(decoded); err != nil {
					return // client disconnected
				}
				c.Writer.(http.Flusher).Flush()
			}
		case <-time.After(5 * time.Minute):
			s.logger.Warn("download stream timeout", zap.String("transfer_id", transferID))
			return
		}
	}
}

func (s *Server) handleFileCreate(c *gin.Context) {
	var req protocol.FileCreatePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.proxyToDeviceRaw(c, protocol.ChannelFile, protocol.ActionFileCreate, req)
}

func (s *Server) handleFileMkdir(c *gin.Context) {
	var req protocol.FileMkdirPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.proxyToDeviceRaw(c, protocol.ChannelFile, protocol.ActionFileMkdir, req)
}

func (s *Server) handleFileDelete(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}
	s.proxyToDevice(c, protocol.ChannelFile, protocol.ActionFileDelete, protocol.FileDeletePayload{Path: path})
}

func (s *Server) handleFileMove(c *gin.Context) {
	var req protocol.FileMovePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.proxyToDeviceRaw(c, protocol.ChannelFile, protocol.ActionFileMove, req)
}

func (s *Server) handleFileChmod(c *gin.Context) {
	var req protocol.FileChmodPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.proxyToDeviceRaw(c, protocol.ChannelFile, protocol.ActionFileChmod, req)
}

func (s *Server) handleFileSearch(c *gin.Context) {
	path := c.Query("path")
	pattern := c.Query("pattern")
	if pattern == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pattern is required"})
		return
	}
	s.proxyToDevice(c, protocol.ChannelFile, protocol.ActionFileSearch, protocol.FileSearchPayload{
		Path:    path,
		Pattern: pattern,
	})
}

// ---- Process handlers ----

func (s *Server) handleProcessList(c *gin.Context) {
	deviceID := c.Param("id")
	if !s.hub.IsDeviceOnline(deviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}
	env, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelProcess, protocol.TypeRequest, protocol.ActionProcessList, nil)
	resp, err := s.hub.RequestToDevice(deviceID, env, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if resp.Error != nil {
		c.JSON(resp.Error.Code, gin.H{"error": resp.Error.Message})
		return
	}
	var result interface{}
	resp.DecodePayload(&result)
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleSystemInfo(c *gin.Context) {
	deviceID := c.Param("id")
	if !s.hub.IsDeviceOnline(deviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}
	env, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelSystem, protocol.TypeRequest, protocol.ActionSystemInfo, nil)
	resp, err := s.hub.RequestToDevice(deviceID, env, 15*time.Second)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if resp.Error != nil {
		c.JSON(resp.Error.Code, gin.H{"error": resp.Error.Message})
		return
	}
	var result interface{}
	resp.DecodePayload(&result)
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleProcessKill(c *gin.Context) {
	var req protocol.ProcessKillPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp := s.proxyToDeviceRaw(c, protocol.ChannelProcess, protocol.ActionProcessKill, req)
	if resp != nil && resp.Error == nil {
		userID, _ := c.Get("user_id")
		s.db.CreateAuditLog(c.Request.Context(), &database.AuditEntry{UserID: userID.(string), DeviceID: c.Param("id"), Action: protocol.ActionProcessKill, Resource: c.Param("pid"), SourceIP: c.ClientIP()})
	}
}

// ---- Tunnel handlers ----

func (s *Server) handleListTunnels(c *gin.Context) {
	// Flush in-memory tunnel byte counters to the database
	s.flushTunnelStats()

	tunnels, err := s.db.ListTunnels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list tunnels"})
		return
	}
	if tunnels == nil {
		tunnels = []*database.Tunnel{}
	}
	// Add socks5_auth flag for each tunnel (password is not exposed)
	type tunnelItem struct {
		*database.Tunnel
		Socks5Auth bool `json:"socks5_auth"`
	}
	items := make([]tunnelItem, len(tunnels))
	for i, t := range tunnels {
		items[i] = tunnelItem{Tunnel: t, Socks5Auth: t.HasSocks5Auth()}
	}
	c.JSON(http.StatusOK, items)
}

// flushTunnelStats writes accumulated tunnel byte counters to the database
func (s *Server) flushTunnelStats() {
	tunnelBytes.Range(func(key, value interface{}) bool {
		tunnelID := key.(string)
		sent, recv := GetAndResetTunnelBytes(tunnelID)
		if sent > 0 || recv > 0 {
			if err := s.db.UpdateTunnelStats(context.Background(), tunnelID, sent, recv); err != nil {
				s.logger.Debug("failed to update tunnel stats", zap.String("tunnel_id", tunnelID), zap.Error(err))
			}
		}
		return true
	})
}

func (s *Server) handleCreateTunnel(c *gin.Context) {
	var req protocol.CreateTunnelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !s.hub.IsDeviceOnline(req.DeviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}
	localAddr, err := s.portPool.Allocate()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no available ports"})
		return
	}
	tunnelID := uuid.New().String()
	userID, _ := c.Get("user_id")

	// Calculate expiration time
	timeout := s.config.Tunnel.PortForwardTimeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	expiresAt := time.Now().Add(timeout).UTC().Format("2006-01-02T15:04:05Z")

	tunnel := &database.Tunnel{ID: tunnelID, DeviceID: req.DeviceID, LocalAddr: localAddr, RemoteAddr: req.RemoteAddr, State: "active", CreatedBy: userID.(string), ExpiresAt: &expiresAt, TunnelType: "port_forward"}
	if err := s.db.CreateTunnel(c.Request.Context(), tunnel); err != nil {
		s.portPool.Release(localAddr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create tunnel"})
		return
	}
	env, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelTunnel, protocol.TypeRequest, protocol.ActionTunnelOpen,
		protocol.TunnelOpenPayload{TunnelID: tunnelID, RemoteAddr: req.RemoteAddr})
	resp, err := s.hub.RequestToDevice(req.DeviceID, env, 10*time.Second)
	if err != nil || (resp != nil && resp.Error != nil) {
		s.portPool.Release(localAddr)
		s.db.CloseTunnel(c.Request.Context(), tunnelID)
		code := http.StatusServiceUnavailable
		msg := err.Error()
		if resp != nil && resp.Error != nil {
			code = resp.Error.Code
			msg = resp.Error.Message
		}
		c.JSON(code, gin.H{"error": msg})
		return
	}
	s.db.CreateAuditLog(c.Request.Context(), &database.AuditEntry{UserID: userID.(string), DeviceID: req.DeviceID, Action: "tunnel.create", Resource: localAddr + " -> " + req.RemoteAddr, SourceIP: c.ClientIP()})

	// Start local TCP listener for this tunnel
	ln, err := s.portPool.Listen(localAddr)
	if err != nil {
		s.logger.Error("failed to listen on tunnel port", zap.String("addr", localAddr), zap.Error(err))
		s.portPool.Release(localAddr)
		s.db.CloseTunnel(c.Request.Context(), tunnelID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start tunnel listener"})
		return
	}
	s.tunnelListeners.Store(tunnelID, ln)
	go s.hub.ServeTunnelListener(tunnelID, req.DeviceID, ln)

	// Start auto-close timer
	s.startTunnelTimer(tunnelID, timeout)

	c.JSON(http.StatusCreated, protocol.CreateTunnelResult{ID: tunnelID, LocalAddr: localAddr, RemoteAddr: req.RemoteAddr, ExpiresAt: &expiresAt})
}

func (s *Server) handleDeleteTunnel(c *gin.Context) {
	id := c.Param("id")
	tunnel, err := s.db.GetTunnel(c.Request.Context(), id)
	if err != nil || tunnel == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tunnel not found"})
		return
	}

	// Cancel auto-close timer
	s.stopTunnelTimer(id)

	// Flush and clean up tunnel byte counters
	sent, recv := GetAndResetTunnelBytes(id)
	tunnelBytes.Delete(id)
	if sent > 0 || recv > 0 {
		s.db.UpdateTunnelStats(c.Request.Context(), id, sent, recv)
	}

	// Close local TCP listener
	if ln, ok := s.tunnelListeners.LoadAndDelete(id); ok {
		ln.(net.Listener).Close()
	}

	env, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelTunnel, protocol.TypeRequest, protocol.ActionTunnelClose,
		protocol.TunnelClosePayload{TunnelID: id})
	s.hub.SendToDevice(tunnel.DeviceID, env)
	s.portPool.Release(tunnel.LocalAddr)
	s.db.CloseTunnel(c.Request.Context(), id)
	userID, _ := c.Get("user_id")
	s.db.CreateAuditLog(c.Request.Context(), &database.AuditEntry{UserID: userID.(string), DeviceID: tunnel.DeviceID, Action: "tunnel.close", Resource: id, SourceIP: c.ClientIP()})
	c.Status(http.StatusNoContent)
}

func (s *Server) handleCreateSocks5(c *gin.Context) {
	var req struct {
		DeviceID   string `json:"device_id" binding:"required"`
		Socks5User string `json:"socks5_user"`
		Socks5Pass string `json:"socks5_pass"`
		AllowedIPs string `json:"allowed_ips"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !s.hub.IsDeviceOnline(req.DeviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}

	// Validate allowed_ips format
	if req.AllowedIPs != "" {
		for _, entry := range strings.Split(req.AllowedIPs, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			if strings.Contains(entry, "/") {
				if _, _, err := net.ParseCIDR(entry); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid CIDR in allowed_ips: %s", entry)})
					return
				}
			} else {
				if net.ParseIP(entry) == nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid IP in allowed_ips: %s", entry)})
					return
				}
			}
		}
	}

	localAddr, err := s.portPool.Allocate()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no available ports"})
		return
	}
	tunnelID := uuid.New().String()
	userID, _ := c.Get("user_id")

	// Calculate expiration time
	timeout := s.config.Tunnel.Socks5Timeout
	if timeout == 0 {
		timeout = 1 * time.Hour
	}
	expiresAt := time.Now().Add(timeout).UTC().Format("2006-01-02T15:04:05Z")

	authEnabled := req.Socks5User != ""

	// Warn about insecure configuration: no auth + no IP whitelist
	if !authEnabled && req.AllowedIPs == "" {
		s.logger.Warn("socks5 tunnel created without auth and without IP whitelist - proxy is publicly accessible",
			zap.String("tunnel_id", tunnelID), zap.String("local_addr", localAddr))
	}

	tunnel := &database.Tunnel{
		ID: tunnelID, DeviceID: req.DeviceID, LocalAddr: localAddr,
		RemoteAddr: "SOCKS5 代理", State: "active", CreatedBy: userID.(string),
		Socks5User: req.Socks5User, Socks5Pass: req.Socks5Pass,
		AllowedIPs: req.AllowedIPs, ExpiresAt: &expiresAt, TunnelType: "socks5",
	}
	if err := s.db.CreateTunnel(c.Request.Context(), tunnel); err != nil {
		s.portPool.Release(localAddr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create socks5 tunnel"})
		return
	}

	// Start SOCKS5 listener - will handle connections via agent
	ln, err := s.portPool.Listen(localAddr)
	if err != nil {
		s.portPool.Release(localAddr)
		s.db.CloseTunnel(c.Request.Context(), tunnelID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start socks5 listener"})
		return
	}
	s.tunnelListeners.Store(tunnelID, ln)
	go s.hub.ServeSocks5Listener(tunnelID, req.DeviceID, ln, req.Socks5User, req.Socks5Pass, req.AllowedIPs)

	// Start auto-close timer
	s.startTunnelTimer(tunnelID, timeout)

	s.db.CreateAuditLog(c.Request.Context(), &database.AuditEntry{UserID: userID.(string), DeviceID: req.DeviceID, Action: "socks5.create", Resource: localAddr, SourceIP: c.ClientIP()})
	c.JSON(http.StatusCreated, gin.H{"id": tunnelID, "local_addr": localAddr, "type": "socks5", "socks5_user": req.Socks5User, "socks5_pass": req.Socks5Pass, "auth": authEnabled, "allowed_ips": req.AllowedIPs, "expires_at": expiresAt})
}

func (s *Server) handleRenewTunnel(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		DurationHours int `json:"duration_hours" binding:"required,min=1,max=5"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "duration_hours must be between 1 and 5"})
		return
	}

	tunnel, err := s.db.GetTunnel(c.Request.Context(), id)
	if err != nil || tunnel == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tunnel not found"})
		return
	}
	if tunnel.State != "active" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tunnel is not active"})
		return
	}
	if tunnel.TunnelType != "socks5" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only SOCKS5 tunnels support renewal"})
		return
	}

	newExpiresAt := time.Now().Add(time.Duration(req.DurationHours) * time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	if err := s.db.UpdateTunnelExpiresAt(c.Request.Context(), id, newExpiresAt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to renew tunnel"})
		return
	}

	// Reset the auto-close timer
	duration := time.Duration(req.DurationHours) * time.Hour
	s.stopTunnelTimer(id)
	s.startTunnelTimer(id, duration)

	userID, _ := c.Get("user_id")
	s.db.CreateAuditLog(c.Request.Context(), &database.AuditEntry{UserID: userID.(string), DeviceID: tunnel.DeviceID, Action: "tunnel.renew", Resource: id, SourceIP: c.ClientIP()})

	c.JSON(http.StatusOK, gin.H{"expires_at": newExpiresAt})
}

func (s *Server) handleHardDeleteTunnel(c *gin.Context) {
	id := c.Param("id")
	tunnel, err := s.db.GetTunnel(c.Request.Context(), id)
	if err != nil || tunnel == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tunnel not found"})
		return
	}
	if tunnel.State == "active" {
		c.JSON(http.StatusConflict, gin.H{"error": "cannot permanently delete active tunnel, close it first"})
		return
	}

	if err := s.db.DeleteTunnel(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete tunnel"})
		return
	}

	userID, _ := c.Get("user_id")
	s.db.CreateAuditLog(c.Request.Context(), &database.AuditEntry{UserID: userID.(string), DeviceID: tunnel.DeviceID, Action: "tunnel.delete", Resource: id, SourceIP: c.ClientIP()})
	c.Status(http.StatusNoContent)
}

// startTunnelTimer starts an auto-close timer for the given tunnel.
// It stops any existing timer for the same tunnelID to prevent timer leaks.
func (s *Server) startTunnelTimer(tunnelID string, duration time.Duration) {
	// Create new timer first
	timer := time.AfterFunc(duration, func() {
		s.autoCloseTunnel(tunnelID)
	})
	// Atomically swap: store new, stop old if present
	if old, loaded := s.tunnelTimers.Swap(tunnelID, timer); loaded {
		old.(*time.Timer).Stop()
	}
}

// stopTunnelTimer stops and removes the auto-close timer for the given tunnel
func (s *Server) stopTunnelTimer(tunnelID string) {
	if val, ok := s.tunnelTimers.LoadAndDelete(tunnelID); ok {
		val.(*time.Timer).Stop()
	}
}

// autoCloseTunnel is called when a tunnel's auto-close timer fires
func (s *Server) autoCloseTunnel(tunnelID string) {
	s.tunnelTimers.Delete(tunnelID)

	// Flush and clean up tunnel byte counters
	sent, recv := GetAndResetTunnelBytes(tunnelID)
	tunnelBytes.Delete(tunnelID)
	if sent > 0 || recv > 0 {
		s.db.UpdateTunnelStats(context.Background(), tunnelID, sent, recv)
	}

	tunnel, err := s.db.GetTunnel(context.Background(), tunnelID)
	if err != nil || tunnel == nil || tunnel.State != "active" {
		return
	}

	s.logger.Info("auto-closing expired tunnel", zap.String("tunnel_id", tunnelID))

	// Close local TCP listener
	if ln, ok := s.tunnelListeners.LoadAndDelete(tunnelID); ok {
		ln.(net.Listener).Close()
	}

	// Notify agent
	env, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelTunnel, protocol.TypeRequest, protocol.ActionTunnelClose,
		protocol.TunnelClosePayload{TunnelID: tunnelID})
	s.hub.SendToDevice(tunnel.DeviceID, env)

	// Release port and close in DB
	s.portPool.Release(tunnel.LocalAddr)
	s.db.CloseTunnel(context.Background(), tunnelID)

	s.db.CreateAuditLog(context.Background(), &database.AuditEntry{UserID: tunnel.CreatedBy, DeviceID: tunnel.DeviceID, Action: "tunnel.auto_close", Resource: tunnelID, SourceIP: "system"})
}

// restoreTunnelTimers cleans up active tunnels after server restart.
// On restart, all listeners, port allocations, and agent connections are lost,
// so active tunnels are stale and must be closed in the database.
func (s *Server) restoreTunnelTimers() {
	tunnels, err := s.db.ListActiveTunnelsWithExpiry(context.Background())
	if err != nil {
		s.logger.Warn("failed to restore tunnel timers", zap.Error(err))
		return
	}
	if len(tunnels) == 0 {
		return
	}
	s.logger.Info("closing stale active tunnels from previous session", zap.Int("count", len(tunnels)))
	for _, t := range tunnels {
		s.db.CloseTunnel(context.Background(), t.ID)
		s.logger.Info("closed stale tunnel", zap.String("tunnel_id", t.ID), zap.String("type", t.TunnelType))
	}
}

// ---- User handlers ----

func (s *Server) handleListUsers(c *gin.Context) {
	users, err := s.db.ListUsers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}
	c.JSON(http.StatusOK, users)
}

func (s *Server) handleCreateUser(c *gin.Context) {
	var req struct {
		Username    string `json:"username" binding:"required"`
		Password    string `json:"password" binding:"required,min=6"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role" binding:"required,oneof=admin operator viewer"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing, _ := s.db.GetUserByUsername(c.Request.Context(), req.Username)
	if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}
	user := &database.User{ID: uuid.New().String(), Username: req.Username, Password: string(hashedPassword), DisplayName: req.DisplayName, Role: req.Role, Enabled: true}
	if err := s.db.CreateUser(c.Request.Context(), user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": user.ID, "username": user.Username, "display_name": user.DisplayName, "role": user.Role, "enabled": user.Enabled})
}

func (s *Server) handleUpdateUser(c *gin.Context) {
	id := c.Param("id")
	user, err := s.db.GetUserByID(c.Request.Context(), id)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	var req struct {
		DisplayName *string `json:"display_name"`
		Role        *string `json:"role"`
		Enabled     *bool   `json:"enabled"`
		Password    *string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.Role != nil {
		validRoles := map[string]bool{"admin": true, "operator": true, "viewer": true}
		if !validRoles[*req.Role] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid role, must be admin, operator, or viewer"})
			return
		}
		user.Role = *req.Role
	}
	if req.Enabled != nil {
		user.Enabled = *req.Enabled
	}
	if err := s.db.UpdateUser(c.Request.Context(), user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
		return
	}
	if req.Password != nil {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}
		if err := s.db.UpdateUserPassword(c.Request.Context(), id, string(hashedPassword)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update password"})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": user.ID, "username": user.Username, "display_name": user.DisplayName, "role": user.Role, "enabled": user.Enabled})
}

func (s *Server) handleDeleteUser(c *gin.Context) {
	if err := s.db.DeleteUser(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- Audit handler ----

func (s *Server) handleAuditLogs(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		offset = 0
	}
	entries, total, err := s.db.ListAuditLogs(c.Request.Context(), database.AuditFilter{
		UserID: c.Query("user_id"), DeviceID: c.Query("device_id"), Action: c.Query("action"),
		From: c.Query("from"), To: c.Query("to"), Limit: limit, Offset: offset,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list audit logs"})
		return
	}
	if entries == nil {
		entries = []*database.AuditEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"total": total, "items": entries, "limit": limit, "offset": offset})
}

// ---- WebSocket handlers ----

func (s *Server) handleAgentWebSocket(c *gin.Context) {
	conn, err := s.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	if s.config.Encryption.Enabled && s.pskMgr.IsEncryptionEnabled() {
		// Enhanced handshake with X25519 key exchange
		s.agentEncryptedHandshake(conn, c)
	} else {
		// Legacy cleartext handshake
		s.agentCleartextHandshake(conn, c)
	}
}

// agentEncryptedHandshake performs the v2 encrypted handshake with key exchange
func (s *Server) agentEncryptedHandshake(conn *websocket.Conn, c *gin.Context) {
	// Step 1: Generate challenge with X25519 key exchange
	challengeResult, err := s.pskMgr.GenerateChallengeWithKeyExchange()
	if err != nil {
		s.logger.Error("failed to generate challenge", zap.Error(err))
		conn.Close()
		return
	}

	challenge, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelSystem, protocol.TypeRequest, protocol.ActionAuthChallenge, protocol.AuthChallengePayload{
		Nonce:        challengeResult.Nonce,
		ServerPubKey: challengeResult.ServerPubKey,
		ServerNonce:  challengeResult.ServerNonce,
		Version:      2,
	})
	data, _ := json.Marshal(challenge)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		s.logger.Warn("agent ws: failed to send challenge", zap.Error(err))
		conn.Close()
		return
	}

	// Step 2: Read auth response
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		s.logger.Warn("agent ws: failed to read auth response", zap.Error(err), zap.String("remote", c.Request.RemoteAddr))
		conn.Close()
		return
	}

	var authResp protocol.Envelope
	if err := json.Unmarshal(msg, &authResp); err != nil {
		s.logger.Warn("agent ws: invalid auth message", zap.Error(err), zap.String("remote", c.Request.RemoteAddr))
		conn.Close()
		return
	}
	if authResp.Action != protocol.ActionAuthResponse {
		s.logger.Warn("agent ws: unexpected auth action", zap.String("action", string(authResp.Action)), zap.String("remote", c.Request.RemoteAddr))
		conn.Close()
		return
	}

	var authPayload protocol.AuthResponsePayload
	if err := authResp.DecodePayload(&authPayload); err != nil {
		s.logger.Warn("agent ws: failed to decode auth payload", zap.Error(err), zap.String("remote", c.Request.RemoteAddr))
		conn.Close()
		return
	}

	// Step 3: Verify HMAC
	if err := s.pskMgr.VerifyHMAC(challengeResult.Nonce, authPayload.HMAC); err != nil {
		s.logger.Warn("agent ws: HMAC verification failed", zap.String("device_id", authPayload.DeviceID), zap.String("remote", c.Request.RemoteAddr), zap.Error(err))
		conn.Close()
		return
	}

	conn.SetReadDeadline(time.Time{})
	s.logger.Info("agent authenticated", zap.String("device_id", authPayload.DeviceID))

	// Step 4: Compute shared secret and derive session keys (if agent sent pubkey)
	var agentSession *svcrypto.Session
	var deviceAlias uint32

	if authPayload.AgentPubKey != "" {
		sharedSecret, serverNonce, authTag, keyErr := s.pskMgr.ComputeSharedSecret(challengeResult.Nonce, authPayload.AgentPubKey)
		if keyErr != nil {
			s.logger.Warn("key exchange failed, falling back to cleartext", zap.Error(keyErr))
		} else {
			clientNonce, _ := base64.StdEncoding.DecodeString(authPayload.AgentNonce)

			keys, deriveErr := svcrypto.DeriveKeys(&svcrypto.KeyDerivationContext{
				SharedSecret: sharedSecret,
				SessionLabel: "dbr-nexus-agent-v1",
				ServerNonce:  serverNonce,
				ClientNonce:  clientNonce,
				AuthTag:      authTag,
			})
			if deriveErr != nil {
				s.logger.Warn("key derivation failed, falling back to cleartext", zap.Error(deriveErr))
			} else {
				agentSession, err = svcrypto.NewSession(keys)
				if err != nil {
					s.logger.Warn("session creation failed, falling back to cleartext", zap.Error(err))
					agentSession = nil
				} else {
					deviceAlias = svcrypto.GenerateDeviceAlias()
				}
			}
		}
	} else {
		s.logger.Info("agent does not support encryption, using cleartext", zap.String("device_id", authPayload.DeviceID))
	}

	// Step 5: Send auth success
	statusPayload := map[string]interface{}{"status": "ok", "device_id": authPayload.DeviceID}
	if agentSession != nil {
		statusPayload["encryption"] = "aes-256-gcm"
		statusPayload["device_alias"] = deviceAlias
	}
	authSuccess, _ := protocol.NewEnvelope(
		challenge.ID,
		protocol.ChannelSystem,
		protocol.TypeResponse,
		protocol.ActionAuthResponse,
		statusPayload,
	)

	if agentSession != nil {
		// Send key confirm as encrypted binary frame
		successData, _ := json.Marshal(authSuccess)
		keyConfirmEnv := &protocol.Envelope{
			ID:        challenge.ID,
			Channel:   protocol.ChannelSystem,
			Type:      protocol.TypeResponse,
			Action:    protocol.ActionAuthResponse,
			Payload:   json.RawMessage(successData),
			Timestamp: time.Now().UnixMilli(),
		}
		frame, encErr := agentSession.EncryptFrame(keyConfirmEnv, deviceAlias, "")
		if encErr != nil {
			s.logger.Error("encrypt key confirm failed", zap.Error(encErr))
			conn.Close()
			return
		}
		// Signal encryption start then send encrypted confirm
		helloMsg := map[string]string{"type": "encryption_start", "challenge_id": challenge.ID}
		helloData, _ := json.Marshal(helloMsg)
		if err := conn.WriteMessage(websocket.TextMessage, helloData); err != nil {
			s.logger.Error("failed to send encryption_start", zap.Error(err))
			conn.Close()
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			s.logger.Error("failed to send encrypted key confirm", zap.Error(err))
			conn.Close()
			return
		}
	} else {
		successData, _ := json.Marshal(authSuccess)
		if err := conn.WriteMessage(websocket.TextMessage, successData); err != nil {
			s.logger.Error("failed to send auth success", zap.Error(err))
			conn.Close()
			return
		}
	}

	// Upsert device
	ctx := context.Background()
	remoteIP := extractRemoteIP(c.Request.RemoteAddr)
	ipLocation := lookupIPLocation(s.logger, remoteIP)

	s.db.UpsertDevice(ctx, &database.Device{
		ID:           authPayload.DeviceID,
		Hostname:     authPayload.Hostname,
		OS:           authPayload.OS,
		Arch:         authPayload.Arch,
		AgentVersion: authPayload.AgentVersion,
		IP:           remoteIP,
		IPInternal:   authPayload.IPInternal,
		IPLocation:   ipLocation,
		Status:       "online",
		Labels:       []string{},
		Tags:         []string{},
	})

	agentConn := &AgentConn{
		DeviceID:    authPayload.DeviceID,
		Conn:        conn,
		Send:        make(chan *protocol.Envelope, 256),
		RawSend:     make(chan []byte, 64),
		Hub:         s.hub,
		Session:     agentSession,
		DeviceAlias: deviceAlias,
	}

	// Start obfuscator for encrypted connections
	if agentSession != nil {
		obfCfg := svcrypto.DefaultObfuscatorConfig()
		obfCfg.Enabled = s.config.Encryption.Enabled
		obfCfg.MinDummyInterval = s.config.Encryption.MinDummyInterval
		obfCfg.MaxDummyInterval = s.config.Encryption.MaxDummyInterval
		obfCfg.JitterPercent = s.config.Encryption.JitterPercent
		obf := svcrypto.NewObfuscator(obfCfg, agentSession, func(frame []byte) error {
			select {
			case agentConn.RawSend <- frame:
				return nil
			default:
				return fmt.Errorf("raw send buffer full")
			}
		})
		agentConn.Obfuscator = obf
		obf.Start()
	}

	s.hub.RegisterAgent(agentConn)
}

// agentCleartextHandshake performs the legacy v1 cleartext handshake
func (s *Server) agentCleartextHandshake(conn *websocket.Conn, c *gin.Context) {
	nonce := s.pskMgr.GenerateChallenge()
	challenge, _ := protocol.NewEnvelope(protocol.GenerateID(), protocol.ChannelSystem, protocol.TypeRequest, protocol.ActionAuthChallenge, protocol.AuthChallengePayload{Nonce: nonce})
	data, _ := json.Marshal(challenge)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		s.logger.Warn("agent ws: failed to send challenge", zap.Error(err))
		conn.Close()
		return
	}

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	_, msg, err := conn.ReadMessage()
	if err != nil {
		s.logger.Warn("agent ws: failed to read auth response", zap.Error(err), zap.String("remote", c.Request.RemoteAddr))
		conn.Close()
		return
	}

	var authResp protocol.Envelope
	if err := json.Unmarshal(msg, &authResp); err != nil {
		s.logger.Warn("agent ws: invalid auth message", zap.Error(err), zap.String("remote", c.Request.RemoteAddr))
		conn.Close()
		return
	}
	if authResp.Action != protocol.ActionAuthResponse {
		s.logger.Warn("agent ws: unexpected auth action", zap.String("action", string(authResp.Action)), zap.String("remote", c.Request.RemoteAddr))
		conn.Close()
		return
	}

	var authPayload protocol.AuthResponsePayload
	if err := authResp.DecodePayload(&authPayload); err != nil {
		s.logger.Warn("agent ws: failed to decode auth payload", zap.Error(err), zap.String("remote", c.Request.RemoteAddr))
		conn.Close()
		return
	}
	if err := s.pskMgr.VerifyHMAC(nonce, authPayload.HMAC); err != nil {
		s.logger.Warn("agent ws: HMAC verification failed", zap.String("device_id", authPayload.DeviceID), zap.String("remote", c.Request.RemoteAddr), zap.Error(err))
		conn.Close()
		return
	}

	conn.SetReadDeadline(time.Time{})
	s.logger.Info("agent authenticated", zap.String("device_id", authPayload.DeviceID))

	authSuccess, _ := protocol.NewEnvelope(
		challenge.ID,
		protocol.ChannelSystem,
		protocol.TypeResponse,
		protocol.ActionAuthResponse,
		map[string]string{"status": "ok", "device_id": authPayload.DeviceID},
	)
	successData, _ := json.Marshal(authSuccess)
	if err := conn.WriteMessage(websocket.TextMessage, successData); err != nil {
		conn.Close()
		return
	}

	ctx := context.Background()
	remoteIP := extractRemoteIP(c.Request.RemoteAddr)
	ipLocation := lookupIPLocation(s.logger, remoteIP)

	s.db.UpsertDevice(ctx, &database.Device{
		ID:           authPayload.DeviceID,
		Hostname:     authPayload.Hostname,
		OS:           authPayload.OS,
		Arch:         authPayload.Arch,
		AgentVersion: authPayload.AgentVersion,
		IP:           remoteIP,
		IPInternal:   authPayload.IPInternal,
		IPLocation:   ipLocation,
		Status:       "online",
		Labels:       []string{},
		Tags:         []string{},
	})

	s.hub.RegisterAgent(&AgentConn{DeviceID: authPayload.DeviceID, Conn: conn, Send: make(chan *protocol.Envelope, 256), Hub: s.hub})
}

func (s *Server) handleBrowserWebSocket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(401, gin.H{"error": "token required"})
		return
	}
	claims, err := auth.ValidateJWTFromQuery(s.jwtMgr, token)
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid token"})
		return
	}
	conn, err := s.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	var browserSession *svcrypto.Session

	if s.config.Encryption.Enabled {
		session, encErr := s.browserEncryptedHandshake(conn, token)
		if encErr != nil {
			s.logger.Warn("browser encryption handshake failed, closing connection", zap.Error(encErr))
			conn.Close()
			return
		}
		browserSession = session
	}

	s.hub.RegisterBrowser(&BrowserConn{
		ID:       protocol.GenerateID(),
		UserID:   claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
		Conn:     conn,
		Send:     make(chan *protocol.Envelope, 256),
		Hub:      s.hub,
		Watching: make(map[string]bool),
		Session:  browserSession,
	})
}

// browserEncryptedHandshake performs P-256 ECDH key exchange with the browser
func (s *Server) browserEncryptedHandshake(conn *websocket.Conn, jwtToken string) (*svcrypto.Session, error) {
	privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate P-256 key: %w", err)
	}

	serverNonce := make([]byte, 16)
	if _, err := rand.Read(serverNonce); err != nil {
		return nil, err
	}

	// Step 1: Send server hello with public key
	helloMsg := map[string]interface{}{
		"type":           "encryption_hello",
		"server_pub_key": base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()),
		"server_nonce":   base64.StdEncoding.EncodeToString(serverNonce),
		"version":        2,
	}
	helloData, _ := json.Marshal(helloMsg)
	if err := conn.WriteMessage(websocket.TextMessage, helloData); err != nil {
		return nil, fmt.Errorf("send hello: %w", err)
	}

	// Step 2: Read browser key exchange response
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read key exchange: %w", err)
	}

	var keyExchange struct {
		Type          string `json:"type"`
		BrowserPubKey string `json:"browser_pub_key"`
		BrowserNonce  string `json:"browser_nonce"`
	}
	if err := json.Unmarshal(msg, &keyExchange); err != nil {
		return nil, fmt.Errorf("parse key exchange: %w", err)
	}
	if keyExchange.Type != "encryption_key_exchange" || keyExchange.BrowserPubKey == "" {
		return nil, fmt.Errorf("browser did not provide key exchange data")
	}

	// Step 3: Compute shared secret
	browserPubKeyBytes, err := base64.StdEncoding.DecodeString(keyExchange.BrowserPubKey)
	if err != nil {
		return nil, fmt.Errorf("decode browser public key: %w", err)
	}

	browserPubKey, err := ecdh.P256().NewPublicKey(browserPubKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse browser public key: %w", err)
	}

	sharedSecret, err := privateKey.ECDH(browserPubKey)
	if err != nil {
		return nil, fmt.Errorf("compute ECDH: %w", err)
	}

	// Step 4: Derive session keys
	clientNonce, _ := base64.StdEncoding.DecodeString(keyExchange.BrowserNonce)
	jwtHash := sha256.Sum256([]byte(jwtToken))

	keys, err := svcrypto.DeriveKeys(&svcrypto.KeyDerivationContext{
		SharedSecret: sharedSecret,
		SessionLabel: "dbr-nexus-browser-v1",
		ServerNonce:  serverNonce,
		ClientNonce:  clientNonce,
		AuthTag:      jwtHash[:],
	})
	if err != nil {
		return nil, fmt.Errorf("derive keys: %w", err)
	}

	session, err := svcrypto.NewSession(keys)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Step 5: Send key confirm as encrypted binary frame
	confirmPayload := map[string]string{"type": "encryption_confirm", "status": "ok"}
	confirmJSON, _ := json.Marshal(confirmPayload)
	confirmEnv := &protocol.Envelope{
		ID:        protocol.GenerateID(),
		Channel:   protocol.ChannelSystem,
		Type:      protocol.TypeResponse,
		Action:    "encryption.confirm",
		Payload:   json.RawMessage(confirmJSON),
		Timestamp: time.Now().UnixMilli(),
	}

	frame, err := session.EncryptFrame(confirmEnv, 0, "")
	if err != nil {
		return nil, fmt.Errorf("encrypt confirm: %w", err)
	}

	conn.SetReadDeadline(time.Time{})
	conn.WriteMessage(websocket.BinaryMessage, frame)

	s.logger.Info("browser encryption established")
	return session, nil
}

// ---- Hub callbacks ----

func (s *Server) onAgentConnect(deviceID string, conn *AgentConn) {
	ctx := context.Background()
	s.db.UpdateDeviceStatus(ctx, deviceID, "online")
	s.db.CreateSession(ctx, &database.Session{ID: protocol.GenerateID(), DeviceID: deviceID, RemoteAddr: conn.Conn.RemoteAddr().String()})
	event, _ := protocol.NewEvent(protocol.ChannelSystem, protocol.ActionDeviceOnline, protocol.DeviceOnlinePayload{DeviceID: deviceID, ConnectedAt: time.Now().UnixMilli()})
	s.hub.BroadcastEvent(event)
	s.logger.Info("agent registered", zap.String("device_id", deviceID))
}

func (s *Server) onAgentDisconnect(deviceID string) {
	ctx := context.Background()
	s.db.UpdateDeviceStatus(ctx, deviceID, "offline")
	event, _ := protocol.NewEvent(protocol.ChannelSystem, protocol.ActionDeviceOffline, protocol.DeviceOfflinePayload{DeviceID: deviceID, Reason: "connection lost", DisconnectedAt: time.Now().UnixMilli()})
	s.hub.BroadcastEvent(event)
}

// ---- Helper: proxy request to device ----

func (s *Server) proxyToDevice(c *gin.Context, channel, action string, payload interface{}) {
	deviceID := c.Param("id")
	if !s.hub.IsDeviceOnline(deviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return
	}
	env, _ := protocol.NewEnvelope(protocol.GenerateID(), channel, protocol.TypeRequest, action, payload)
	resp, err := s.hub.RequestToDevice(deviceID, env, 15*time.Second)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if resp.Error != nil {
		c.JSON(resp.Error.Code, gin.H{"error": resp.Error.Message})
		return
	}
	var result interface{}
	resp.DecodePayload(&result)
	c.JSON(http.StatusOK, result)
}

func (s *Server) proxyToDeviceRaw(c *gin.Context, channel, action string, payload interface{}) *protocol.Envelope {
	deviceID := c.Param("id")
	if !s.hub.IsDeviceOnline(deviceID) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "device is offline"})
		return nil
	}
	env, _ := protocol.NewEnvelope(protocol.GenerateID(), channel, protocol.TypeRequest, action, payload)
	resp, err := s.hub.RequestToDevice(deviceID, env, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return nil
	}
	if resp.Error != nil {
		c.JSON(resp.Error.Code, gin.H{"error": resp.Error.Message})
		return resp
	}
	c.Status(http.StatusNoContent)
	return resp
}

// ---- Settings handlers ----

func (s *Server) handleGetSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"server_addr":            s.config.Server.Addr,
		"tunnel_bind_range":      s.config.Tunnel.BindRange,
		"jwt_expiry":             s.config.Auth.JWTExpiry.String(),
		"heartbeat_timeout":      s.config.Agent.HeartbeatTimeout.String(),
		"tls_enabled":            s.config.Server.TLS.Cert != "" && s.config.Server.TLS.Key != "",
		"psk_configured":         s.config.Auth.PSK != "",
		"log_level":              s.config.Logging.Level,
		"web_access_enabled":     s.config.WebAccessControl.Enabled,
		"web_access_allowed_ips": s.config.WebAccessControl.AllowedIPs,
	})
}

func (s *Server) handleUpdateSettings(c *gin.Context) {
	var req struct {
		TunnelBindRange     *string `json:"tunnel_bind_range"`
		HeartbeatTimeout    *string `json:"heartbeat_timeout"`
		LogLevel            *string `json:"log_level"`
		WebAccessEnabled    *bool   `json:"web_access_enabled"`
		WebAccessAllowedIPs *string `json:"web_access_allowed_ips"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.TunnelBindRange != nil {
		pp, err := NewPortPool(*req.TunnelBindRange)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tunnel bind range: " + err.Error()})
			return
		}
		s.config.Tunnel.BindRange = *req.TunnelBindRange
		s.portPool = pp
	}
	if req.HeartbeatTimeout != nil {
		d, err := time.ParseDuration(*req.HeartbeatTimeout)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid heartbeat timeout"})
			return
		}
		s.config.Agent.HeartbeatTimeout = d
	}
	if req.LogLevel != nil {
		s.config.Logging.Level = *req.LogLevel
	}
	if req.WebAccessEnabled != nil {
		s.config.WebAccessControl.Enabled = *req.WebAccessEnabled
	}
	if req.WebAccessAllowedIPs != nil {
		// Validate IPs before applying
		if *req.WebAccessAllowedIPs != "" {
			for _, entry := range strings.Split(*req.WebAccessAllowedIPs, ",") {
				entry = strings.TrimSpace(entry)
				if entry == "" {
					continue
				}
				if strings.Contains(entry, "/") {
					if _, _, err := net.ParseCIDR(entry); err != nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid CIDR: %s", entry)})
						return
					}
				} else {
					if net.ParseIP(entry) == nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid IP: %s", entry)})
						return
					}
				}
			}
		}
		s.config.WebAccessControl.AllowedIPs = *req.WebAccessAllowedIPs
	}
	userID, _ := c.Get("user_id")
	s.db.CreateAuditLog(c.Request.Context(), &database.AuditEntry{UserID: userID.(string), Action: "settings.update", Resource: "system", SourceIP: c.ClientIP()})
	c.JSON(http.StatusOK, gin.H{"message": "settings updated"})
}

// serveFrontend serves the React SPA static files with fallback to index.html
func (s *Server) serveFrontend(router *gin.Engine) {
	distDir := "./web/dist"
	if envDir := os.Getenv("WEB_DIST_DIR"); envDir != "" {
		distDir = envDir
	}

	// Check if dist directory exists
	if info, err := os.Stat(distDir); err != nil || !info.IsDir() {
		s.logger.Warn("frontend dist directory not found, UI will not be available",
			zap.String("path", distDir),
		)
		return
	}

	s.logger.Info("serving frontend", zap.String("dir", distDir))

	// Serve static assets
	router.Use(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Skip API and WebSocket routes
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/") || path == "/health" {
			c.Next()
			return
		}

		// Apply web IP whitelist to frontend static files
		cfg := s.config.WebAccessControl
		if cfg.Enabled && cfg.AllowedIPs != "" {
			// Use RemoteIP() to avoid X-Forwarded-For spoofing (same as IPWhitelistMiddleware)
			if !auth.IsIPAllowed(c.RemoteIP(), cfg.AllowedIPs) {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
		}

		// Try to serve the static file
		filePath := filepath.Join(distDir, path)
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			c.File(filePath)
			c.Abort()
			return
		}

		// SPA fallback: serve index.html for all other routes
		indexPath := filepath.Join(distDir, "index.html")
		c.File(indexPath)
		c.Abort()
	})
}

// agentBuildParams holds the parameters for building an agent binary
type agentBuildParams struct {
	Platform  string
	Arch      string
	ServerURL string
	PSK       string
}

// buildAgentBinary cross-compiles an agent binary and returns the temp file path.
// The caller is responsible for removing the temp file.
func (s *Server) buildAgentBinary(ctx context.Context, params agentBuildParams, isTLS bool) (tmpPath string, filename string, err error) {
	// Build server URL - use wss:// if TLS is detected (direct or via reverse proxy)
	serverURL := params.ServerURL
	if !strings.HasPrefix(serverURL, "ws://") && !strings.HasPrefix(serverURL, "wss://") {
		if isTLS {
			serverURL = "wss://" + serverURL
		} else {
			serverURL = "ws://" + serverURL
		}
	}
	if !strings.HasSuffix(serverURL, "/ws/agent") {
		serverURL = serverURL + "/ws/agent"
	}

	// Use PSK from request or server's configured PSK
	psk := params.PSK
	if psk == "" {
		psk = s.config.Auth.PSK
	}

	// Find project root (where go.mod is) - walk up from executable or working dir
	projectRoot := ""
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exePath))
	}
	for _, dir := range candidates {
		for i := 0; i < 5; i++ {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				projectRoot = dir
				break
			}
			dir = filepath.Dir(dir)
		}
		if projectRoot != "" {
			break
		}
	}
	if projectRoot == "" {
		return "", "", fmt.Errorf("cannot find project root (go.mod not found)")
	}

	// Create temp file for the output binary
	ext := ""
	if params.Platform == "windows" {
		ext = ".exe"
	}
	tmpFile, err := os.CreateTemp("", "agent-build-*"+ext)
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath = tmpFile.Name()
	tmpFile.Close()

	// Build the agent binary with embedded config
	ldflags := fmt.Sprintf("-X main.defaultServerURL=%s -X main.defaultPSK=%s", serverURL, psk)
	cmd := exec.CommandContext(ctx, "go", "build",
		"-trimpath",
		"-ldflags", ldflags,
		"-o", tmpPath,
		"./cmd/agent/",
	)
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"GOOS="+params.Platform,
		"GOARCH="+params.Arch,
		"CGO_ENABLED=0",
	)

	output, buildErr := cmd.CombinedOutput()
	if buildErr != nil {
		os.Remove(tmpPath)
		s.logger.Error("agent build failed",
			zap.Error(buildErr),
			zap.String("output", string(output)),
			zap.String("platform", params.Platform),
			zap.String("arch", params.Arch),
		)
		return "", "", fmt.Errorf("build failed: %s", string(output))
	}

	// Use .bin extension for non-Windows builds to prevent browsers from appending .txt
	downloadExt := ext
	if downloadExt == "" {
		downloadExt = ".bin"
	}
	filename = fmt.Sprintf("qoder-agent-%s-%s%s", params.Platform, params.Arch, downloadExt)
	return tmpPath, filename, nil
}

// handleAgentBuild cross-compiles an agent binary and returns it as a download (POST with JSON body)
func (s *Server) handleAgentBuild(c *gin.Context) {
	var req struct {
		Platform  string `json:"platform"`   // windows, linux
		Arch      string `json:"arch"`       // amd64, arm64
		ServerURL string `json:"server_url"` // host:port
		PSK       string `json:"psk"`        // pre-shared key
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Validate platform and arch
	validPlatforms := map[string]bool{"windows": true, "linux": true, "darwin": true}
	validArchs := map[string]bool{"amd64": true, "arm64": true}
	if !validPlatforms[req.Platform] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported platform, use: windows, linux, darwin"})
		return
	}
	if !validArchs[req.Arch] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported arch, use: amd64, arm64"})
		return
	}
	if req.ServerURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_url is required"})
		return
	}

	tmpPath, filename, err := s.buildAgentBinary(c.Request.Context(), agentBuildParams{
		Platform:  req.Platform,
		Arch:      req.Arch,
		ServerURL: req.ServerURL,
		PSK:       req.PSK,
	}, c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer os.Remove(tmpPath)

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Transfer-Encoding", "binary")
	c.File(tmpPath)
}

// handleAgentDownload cross-compiles an agent binary and returns it as a download (GET with query params)
// This endpoint is designed for one-line install commands (curl/wget/powershell).
func (s *Server) handleAgentDownload(c *gin.Context) {
	platform := c.Query("platform")
	arch := c.Query("arch")
	serverURL := c.Query("server_url")
	psk := c.Query("psk")

	// Validate platform and arch
	validPlatforms := map[string]bool{"windows": true, "linux": true, "darwin": true}
	validArchs := map[string]bool{"amd64": true, "arm64": true}
	if !validPlatforms[platform] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported platform, use: windows, linux, darwin"})
		return
	}
	if !validArchs[arch] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported arch, use: amd64, arm64"})
		return
	}
	if serverURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_url is required"})
		return
	}

	tmpPath, filename, err := s.buildAgentBinary(c.Request.Context(), agentBuildParams{
		Platform:  platform,
		Arch:      arch,
		ServerURL: serverURL,
		PSK:       psk,
	}, c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer os.Remove(tmpPath)

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Transfer-Encoding", "binary")
	c.File(tmpPath)
}
