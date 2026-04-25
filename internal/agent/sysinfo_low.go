package agent

import (
	"net"
	"os"
	"runtime"
	"strings"
)

// hostname returns the system hostname
func hostname() (string, error) {
	return os.Hostname()
}

// operatingSystem returns the OS name
func operatingSystem() string {
	return runtime.GOOS
}

// architecture returns the system architecture
func architecture() string {
	return runtime.GOARCH
}

// getInternalIPs returns the non-loopback IPv4 addresses of the host
func getInternalIPs() string {
	var ips []string
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				ips = append(ips, ipv4.String())
			}
		}
	}
	return strings.Join(ips, ", ")
}
