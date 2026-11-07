package quadlet

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/spec"
)

// noNetworkWait drops Quadlet's default dependency on the system's
// network-online.target (D29). On Debian 13 nothing pulls that target in, so
// podman-user-wait-network-online.service runs into its 90 s timeout and
// delays every container start, at boot and at deploy time. dermaga's
// containers need no network to start: images are local (Pull=never).
var noNetworkWait = section{"Quadlet", [][2]string{{"DefaultDependencies", "false"}}}

// Release is one deployable build of an app: the container dg-<app>-<rel8>.
type Release struct {
	App       spec.App
	Rel8      string   // 8 lowercase hex characters, random per release
	Image     string   // localhost/dermaga/<app>:<sha12>, or a pinned ref for image apps
	EnvFile   string   // absolute path of ~/.config/dermaga/env/<app>.env
	Databases []string // linked databases: the app joins dg-db-<name>.network
	// ImageHealthcheck: the image defines a HEALTHCHECK, so Notify=healthy works without HealthCmd.
	ImageHealthcheck bool
}

const (
	stopTimeout  = 10 // s: SIGTERM grace before podman kills (StopTimeout=, D3)
	stopMargin   = 20 // s: TimeoutStopSec= = StopTimeout + this, so systemd never kills podman mid-stop
	startTimeout = 120
)

var (
	rel8RE = regexp.MustCompile(`^[0-9a-f]{8}$`)
	// absolute paths without spaces, % or $: they go into unit values verbatim
	absPathRE = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)+$`)
)

// ContainerName is the container and unit base name of a release.
func ContainerName(app, rel8 string) string { return name.Unit + app + "-" + rel8 }

// Container renders dg-<app>-<rel8>.container (BRIEF §3.5 step 5).
func Container(r Release) (filename string, data []byte, err error) {
	a := r.App
	if err := a.Validate(); err != nil {
		return "", nil, err
	}
	if !rel8RE.MatchString(r.Rel8) {
		return "", nil, fmt.Errorf("release id %q: want 8 lowercase hex characters", r.Rel8)
	}
	if !spec.ValidImage(r.Image) {
		return "", nil, fmt.Errorf("image %q: not a fully qualified image reference", r.Image)
	}
	if !absPathRE.MatchString(r.EnvFile) {
		return "", nil, fmt.Errorf("env file %q: not a plain absolute path", r.EnvFile)
	}
	cname := ContainerName(a.Name, r.Rel8)
	lim := a.Limits.Effective()

	c := [][2]string{
		{"ContainerName", cname},
		{"Image", r.Image},
		{"Pull", "never"}, // dermaga builds or pulls before creating the unit; a boot never waits for a registry
		{"Network", "web.network"},
	}
	for _, db := range r.Databases {
		if !spec.ValidName(db) {
			return "", nil, fmt.Errorf("database %q: invalid name", db)
		}
		c = append(c, [2]string{"Network", DBNetwork(db)})
	}
	c = append(c,
		[2]string{"EnvironmentFile", r.EnvFile},
		[2]string{"Label", name.Name + ".app=" + a.Name},
		[2]string{"Label", name.Name + ".release=" + r.Rel8},
		[2]string{"NoNewPrivileges", "true"},
		[2]string{"DropCapability", "ALL"},
	)
	if len(a.AddCaps) > 0 {
		c = append(c, [2]string{"AddCapability", strings.Join(a.AddCaps, " ")})
	}
	if a.ReadOnly {
		c = append(c, [2]string{"ReadOnly", "true"})
	}
	c = append(c,
		[2]string{"PidsLimit", strconv.Itoa(lim.Pids)},
		[2]string{"PodmanArgs", "--memory=" + lim.Memory + " --cpus=" + lim.CPUs}, // D4: Quadlet rejects Memory=
		[2]string{"StopTimeout", strconv.Itoa(stopTimeout)},
	)
	for _, v := range a.Volumes {
		c = append(c, [2]string{"Volume", VolumeUnit(a.Name, v.Name) + ":" + v.Path})
	}
	if a.Health.Cmd != "" {
		c = append(c, [2]string{"HealthCmd", a.Health.Cmd})
	}
	if a.Health.Cmd != "" || r.ImageHealthcheck {
		c = append(c, [2]string{"Notify", "healthy"})
	}
	start := a.Health.TimeoutSec
	if start == 0 {
		start = startTimeout
	}
	data, err = unit{
		noNetworkWait,
		{"Unit", [][2]string{{"Description", fmt.Sprintf("%s app %s, release %s", name.Name, a.Name, r.Rel8)}}},
		{"Container", c},
		{"Service", [][2]string{
			{"Restart", "always"},
			{"TimeoutStartSec", strconv.Itoa(start)},
			{"TimeoutStopSec", strconv.Itoa(stopTimeout + stopMargin)},
		}},
		{"Install", [][2]string{{"WantedBy", "default.target"}}},
	}.render()
	return cname + ".container", data, err
}

// VolumeUnit is the Quadlet file name of an app's named volume.
func VolumeUnit(app, vol string) string { return name.Unit + app + "_" + vol + ".volume" }

// Volume renders dg-<app>_<vol>.volume. dermaga never removes volumes on its own.
func Volume(app string, v spec.Volume) (filename string, data []byte, err error) {
	if !spec.ValidName(app) || !spec.ValidName(v.Name) {
		return "", nil, fmt.Errorf("volume %q of app %q: invalid name", v.Name, app)
	}
	fn := VolumeUnit(app, v.Name)
	data, err = unit{noNetworkWait, {"Volume", [][2]string{
		{"VolumeName", strings.TrimSuffix(fn, ".volume")},
		{"Label", name.Name + ".app=" + app},
	}}}.render()
	return fn, data, err
}

// DBNetwork is the Quadlet file name of a database's private network.
func DBNetwork(db string) string { return name.Unit + "db-" + db + ".network" }

// Network renders dg-db-<name>.network. Databases join only this network,
// never web (BRIEF §3.7); the engines' own units come in M5.
func Network(db string) (filename string, data []byte, err error) {
	if !spec.ValidName(db) {
		return "", nil, fmt.Errorf("database %q: invalid name", db)
	}
	fn := DBNetwork(db)
	data, err = unit{noNetworkWait, {"Network", [][2]string{
		{"NetworkName", strings.TrimSuffix(fn, ".network")},
		{"Label", name.Name + ".database=" + db},
	}}}.render()
	return fn, data, err
}

// Hooks renders dg-hooks.container (ARCHITECTURE D1): the relay runs the
// dermaga binary copied into rootfsDir and forwards :8080 to the hook socket
// in socketDir. The directory is mounted, not the socket file: a bind-mounted
// socket file would keep pointing at the old inode after a daemon restart.
func Hooks(rootfsDir, socketDir string) (filename string, data []byte, err error) {
	if !absPathRE.MatchString(rootfsDir) || !absPathRE.MatchString(socketDir) {
		return "", nil, fmt.Errorf("hook relay paths %q, %q: not plain absolute paths", rootfsDir, socketDir)
	}
	data, err = unit{
		noNetworkWait,
		{"Unit", [][2]string{{"Description", name.Name + " hook relay (Traefik to hooks.sock)"}}},
		{"Container", [][2]string{
			{"ContainerName", name.Unit + "hooks"},
			{"Rootfs", rootfsDir + ":O"},
			{"Exec", "/" + name.Name + " hook-relay -listen :8080 -target " + RelaySocket},
			{"Network", "web.network"},
			{"Volume", socketDir + ":" + filepath.Dir(RelaySocket)},
			{"ReadOnly", "true"},
			{"NoNewPrivileges", "true"},
			{"DropCapability", "ALL"},
			{"PidsLimit", "32"},
			{"PodmanArgs", "--memory=32m"},
			{"StopTimeout", "5"},
		}},
		{"Service", [][2]string{{"Restart", "always"}, {"TimeoutStopSec", "25"}}},
		{"Install", [][2]string{{"WantedBy", "default.target"}}},
	}.render()
	return name.Unit + "hooks.container", data, err
}

// RelaySocket is the hook socket's path inside dg-hooks.
const RelaySocket = "/run/" + name.Name + "/hooks.sock"
