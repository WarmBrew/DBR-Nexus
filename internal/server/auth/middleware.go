package auth

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Permission constants
const (
	PermViewDevices   = "view_devices"
	PermShell         = "shell"
	PermFileBrowse    = "file_browse"
	PermFileWrite     = "file_write"
	PermProcessKill   = "process_kill"
	PermTunnelCreate  = "tunnel_create"
	PermManageUsers   = "manage_users"
	PermViewAuditLogs = "view_audit_logs"
	PermDeleteDevices = "delete_devices"
)

// rolePermissions maps roles to their permissions
var rolePermissions = map[string]map[string]bool{
	"admin": {
		PermViewDevices:   true,
		PermShell:         true,
		PermFileBrowse:    true,
		PermFileWrite:     true,
		PermProcessKill:   true,
		PermTunnelCreate:  true,
		PermManageUsers:   true,
		PermViewAuditLogs: true,
		PermDeleteDevices: true,
	},
	"operator": {
		PermViewDevices:  true,
		PermShell:        true,
		PermFileBrowse:   true,
		PermFileWrite:    true,
		PermProcessKill:  true,
		PermTunnelCreate: true,
	},
	"viewer": {
		PermViewDevices: true,
		PermFileBrowse:  true,
	},
}

// JWTMiddleware validates JWT token from Authorization header or query parameter
func JWTMiddleware(jwtMgr *JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := ""

		// Try Authorization header first
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenStr = parts[1]
			}
		}

		// Fall back to query parameter (for file downloads via direct URL)
		if tokenStr == "" {
			if t := c.Query("token"); t != "" {
				tokenStr = t
			}
		}

		if tokenStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authorization header required"})
			c.Abort()
			return
		}

		claims, err := jwtMgr.ValidateToken(tokenStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		// Store claims in context
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Set("jwt_token", tokenStr)

		c.Next()
	}
}

// RequirePermission checks if the user has the required permission
func RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			c.Abort()
			return
		}

		roleStr := role.(string)
		perms, ok := rolePermissions[roleStr]
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "unknown role"})
			c.Abort()
			return
		}

		if !perms[permission] {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ValidateJWTFromQuery validates a JWT token from query parameter (for WebSocket)
func ValidateJWTFromQuery(jwtMgr *JWTManager, token string) (*Claims, error) {
	return jwtMgr.ValidateToken(token)
}

// HasPermission checks if a role has a specific permission
func HasPermission(role, permission string) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[permission]
}

// WebAccessControlConfig is the config snapshot needed by the IP whitelist middleware
type WebAccessControlConfig struct {
	Enabled    bool
	AllowedIPs string
}

// IsIPAllowed checks whether the given IP string is allowed by the whitelist.
// It always allows loopback addresses. allowedIPs is a comma-separated list
// of IP addresses or CIDR ranges. Returns true if allowedIPs is empty.
func IsIPAllowed(ipStr string, allowedIPs string) bool {
	if allowedIPs == "" {
		return true
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		host, _, err := net.SplitHostPort(ipStr)
		if err != nil {
			host = ipStr
		}
		ip = net.ParseIP(host)
	}
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, entry := range strings.Split(allowedIPs, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			if network.Contains(ip) {
				return true
			}
		} else {
			allowedIP := net.ParseIP(entry)
			if allowedIP != nil && allowedIP.Equal(ip) {
				return true
			}
		}
	}
	return false
}

// IPWhitelistMiddleware returns a middleware that restricts web access to allowed IPs.
// getConfig is called on every request so the middleware respects live config changes.
// Only applies to web (API/frontend) routes, not to agent WebSocket connections.
func IPWhitelistMiddleware(getConfig func() WebAccessControlConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := getConfig()
		if !cfg.Enabled || cfg.AllowedIPs == "" {
			c.Next()
			return
		}

		// Use RemoteIP() instead of ClientIP() to avoid X-Forwarded-For spoofing.
		// ClientIP() trusts proxy headers which can be forged by attackers to bypass the whitelist.
		if IsIPAllowed(c.RemoteIP(), cfg.AllowedIPs) {
			c.Next()
			return
		}

		// Not in whitelist — return 404 to avoid revealing the server exists
		c.AbortWithStatus(http.StatusNotFound)
	}
}
