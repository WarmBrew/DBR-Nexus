package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ipLocationCache caches IP geolocation results
var ipLocationCache sync.Map // ip string -> ipLocationEntry

type ipLocationEntry struct {
	Location string
	ExpireAt time.Time
}

// ipLookupResult represents the response from ip-api.com
type ipLookupResult struct {
	Status  string `json:"status"`
	Country string `json:"country"`
	Region  string `json:"regionName"`
	City    string `json:"city"`
	Query   string `json:"query"`
}

// lookupIPLocation queries ip-api.com for the geographic location of an IP
// Returns a formatted string like "中国 广东省 深圳" or empty string on failure
func lookupIPLocation(logger *zap.Logger, ipStr string) string {
	// Don't look up private/local IPs
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ""
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return ""
	}

	// Check cache first
	if entry, ok := ipLocationCache.Load(ipStr); ok {
		e := entry.(ipLocationEntry)
		if time.Now().Before(e.ExpireAt) {
			return e.Location
		}
		ipLocationCache.Delete(ipStr)
	}

	// Query ip-api.com (free, no API key, 45 req/min limit)
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,regionName,city,query&lang=zh-CN", ipStr)
	resp, err := client.Get(url)
	if err != nil {
		logger.Debug("ip location lookup failed", zap.String("ip", ipStr), zap.Error(err))
		return ""
	}
	defer resp.Body.Close()

	var result ipLookupResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Debug("ip location decode failed", zap.String("ip", ipStr), zap.Error(err))
		return ""
	}

	if result.Status != "success" {
		return ""
	}

	// Build location string
	location := result.Country
	if result.Region != "" {
		location += " " + result.Region
	}
	if result.City != "" && result.City != result.Region {
		location += " " + result.City
	}

	// Cache for 24 hours
	ipLocationCache.Store(ipStr, ipLocationEntry{
		Location: location,
		ExpireAt: time.Now().Add(24 * time.Hour),
	})

	logger.Info("ip location resolved", zap.String("ip", ipStr), zap.String("location", location))
	return location
}

// extractRemoteIP extracts the real IP from a remote address (strips port)
func extractRemoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
