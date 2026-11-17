package quadlet

import (
	"fmt"
	"path/filepath"
	"strings"
)

// File names of the Traefik units. They follow HOST-6's names; ownership is shown by the marker
// on the first line (DEC-17), not by a prefix.
const (
	TraefikContainerFile   = "traefik.container"
	TraefikHTTPSocketFile  = "traefik-http.socket"
	TraefikHTTPSSocketFile = "traefik-https.socket"
	TraefikServiceName     = "traefik.service"
)

// TraefikSpec defines the Traefik reverse proxy container (HOST-6).
type TraefikSpec struct {
	Image      string
	ConfigDir  string // host directory mounted at /etc/traefik: traefik.yml and acme.json
	DynamicDir string // host directory mounted read-only at /etc/traefik/dynamic
}

// SocketSpec defines a systemd user socket that owns a public port and hands it to Traefik.
type SocketSpec struct {
	Description        string
	ListenStream       string
	ListenDatagram     string
	FileDescriptorName string // must match the Traefik entry point name
}

// TraefikHTTPSocket owns TCP 80 for the "web" entry point.
func TraefikHTTPSocket() SocketSpec {
	return SocketSpec{
		Description:        "dermaga Traefik :80 (HTTP to HTTPS redirect, ACME challenges)",
		ListenStream:       "80",
		FileDescriptorName: "web",
	}
}

// TraefikHTTPSSocket owns TCP and UDP 443 for the "websecure" entry point; the shared
// FileDescriptorName gives Traefik the UDP socket for HTTP/3 (DEC-04).
func TraefikHTTPSSocket() SocketSpec {
	return SocketSpec{
		Description:        "dermaga Traefik :443 (HTTPS over TCP, HTTP/3 over UDP)",
		ListenStream:       "443",
		ListenDatagram:     "443",
		FileDescriptorName: "websecure",
	}
}

// UnitMarker returns the ownership marker of a fixed infrastructure unit, e.g. traefik.container.
func UnitMarker(fileName string) string {
	return "# managed by dermaga; unit=" + fileName
}

// HasUnitMarker reports whether content starts with the ownership marker of fileName.
func HasUnitMarker(content, fileName string) bool {
	first, _, _ := strings.Cut(content, "\n")
	return strings.TrimSpace(first) == UnitMarker(fileName)
}

// RenderTraefik renders the Traefik container unit. Traefik gets no Podman socket (SEC-5), uses
// the file provider only, and receives :80/:443 from the user-manager sockets (HOST-6).
func RenderTraefik(spec TraefikSpec) ([]byte, error) {
	if !imageRefRegex.MatchString(spec.Image) {
		return nil, fmt.Errorf("invalid traefik image reference %q", spec.Image)
	}
	for _, dir := range []string{spec.ConfigDir, spec.DynamicDir} {
		if !filepath.IsAbs(dir) || strings.ContainsAny(dir, " \t\r\n\x00%\\:`'\"$;|&<>") {
			return nil, fmt.Errorf("invalid traefik mount directory %q", dir)
		}
	}

	sockets := TraefikHTTPSocketFile + " " + TraefikHTTPSSocketFile
	doc := iniDocument{HeaderComment: UnitMarker(TraefikContainerFile)}

	unit := doc.addSection("Unit")
	unit.add("Description", "dermaga Traefik reverse proxy (socket-activated)")
	unit.add("Requires", sockets)
	unit.add("After", sockets)

	c := doc.addSection("Container")
	c.add("ContainerName", "traefik")
	c.add("Image", spec.Image)
	c.add("Network", NetworkFileName("web"))
	c.add("Notify", "true")
	c.add("NoNewPrivileges", "true")
	c.add("ReadOnly", "true")
	c.add("DropCapability", "ALL")
	c.add("PidsLimit", "256")
	c.add("PodmanArgs", "--memory=256m")
	// A directory mount, never single files: atomic renames of traefik.yml stay visible, and
	// Traefik rewrites acme.json in place (traefik_acme_source_check.log).
	c.add("Volume", spec.ConfigDir+":/etc/traefik")
	c.add("Volume", spec.DynamicDir+":/etc/traefik/dynamic:ro")

	q := doc.addSection("Quadlet")
	q.add("DefaultDependencies", "false") // DEC-06

	s := doc.addSection("Service")
	s.add("Sockets", sockets)
	s.add("Restart", "always")
	s.add("TimeoutStartSec", "300") // the first start pulls the image

	inst := doc.addSection("Install")
	inst.add("WantedBy", "default.target")

	return doc.render()
}

// RenderSocket renders a systemd socket unit for Traefik. Specs come from this package's
// constructors, not from users.
func RenderSocket(fileName string, spec SocketSpec) ([]byte, error) {
	doc := iniDocument{HeaderComment: UnitMarker(fileName)}

	unit := doc.addSection("Unit")
	unit.add("Description", spec.Description)

	s := doc.addSection("Socket")
	s.add("ListenStream", spec.ListenStream)
	s.add("ListenDatagram", spec.ListenDatagram)
	s.add("FileDescriptorName", spec.FileDescriptorName)
	s.add("Service", TraefikServiceName)

	inst := doc.addSection("Install")
	inst.add("WantedBy", "sockets.target")

	return doc.render()
}
