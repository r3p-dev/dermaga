package host

import (
	"net"
	"os"
	"path/filepath"
	"strings"
)

// SSHPort is the port sshd listens on, read from its configuration. The CLI's
// panel snippet, the panel's Settings snippet and the database tunnel snippet
// all use it.
func SSHPort() string {
	files, _ := filepath.Glob("/etc/ssh/sshd_config.d/*.conf")
	return SSHPortFrom(append(files, "/etc/ssh/sshd_config"))
}

// SSHPortFrom returns the first Port directive in files, in the order sshd
// reads them (Debian includes sshd_config.d/*.conf before its own settings).
func SSHPortFrom(files []string) string {
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if fl := strings.Fields(l); len(fl) == 2 && strings.EqualFold(fl[0], "Port") {
				return fl[1]
			}
		}
	}
	return "22"
}

// Address is the address of the interface this server uses to reach the
// internet, and whether it is a public one. No packet is sent: connecting a
// UDP socket only asks the kernel for the route. A server behind NAT (many
// clouds, every home network) only knows its private address, so public is
// false and the caller must not present the address as the one to SSH to.
func Address() (addr string, public bool) {
	c, err := net.Dial("udp", "192.0.2.1:9") // TEST-NET-1: never routed, never contacted
	if err != nil {
		return "", false
	}
	defer c.Close()
	ip := c.LocalAddr().(*net.UDPAddr).IP
	return ip.String(), publicIP(ip)
}

func publicIP(ip net.IP) bool {
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10") // carrier-grade NAT and Tailscale
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !cgnat.Contains(ip)
}
