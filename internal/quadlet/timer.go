package quadlet

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/r3p-dev/dermaga/internal/name"
)

// Timer is a scheduled dermaga command: dg-<Name>.timer starts dg-<Name>.service,
// which runs Binary with Args (e.g. backup run --scheduled db:shop, BRIEF §3.8).
type Timer struct {
	Name        string   // e.g. backup-db-shop
	Description string   // e.g. "backup db:shop"
	Binary      string   // absolute path of the dermaga binary
	Args        []string // e.g. backup run --scheduled db:shop
	OnCalendar  string   // systemd calendar spec, e.g. "*-*-* 03:00:00 Asia/Jakarta"
	RandomDelay int      // seconds (RandomizedDelaySec=)
	OnFailure   string   // optional failure unit, e.g. dg-backup-failed@%n.service
}

var (
	timerNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}[a-z0-9]$`)
	// ExecStart splits on spaces and interprets quotes and backslashes: allow none of them
	argRE      = regexp.MustCompile(`^[A-Za-z0-9_.:/=@+-]+$`)
	calendarRE = regexp.MustCompile(`^[A-Za-z0-9 *:,./~_+-]{1,128}$`)
	descRE     = regexp.MustCompile(`^[A-Za-z0-9 :,./_()+-]{1,128}$`)
	hhmmRE     = regexp.MustCompile(`^[0-2][0-9]:[0-5][0-9]$`)
)

// FormatCalendar normalizes a schedule (e.g. "03:00") into a systemd calendar spec.
func FormatCalendar(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "03:00"
	}
	if hhmmRE.MatchString(s) {
		return "*-*-* " + s + ":00 Asia/Jakarta"
	}
	return s
}

// Units renders dg-<Name>.service and dg-<Name>.timer (for ~/.config/systemd/user).
func (t Timer) Units() (service, timer []byte, err error) {
	switch {
	case !timerNameRE.MatchString(t.Name):
		return nil, nil, fmt.Errorf("timer name %q: invalid", t.Name)
	case !descRE.MatchString(t.Description):
		return nil, nil, fmt.Errorf("timer description %q: invalid", t.Description)
	case !absPathRE.MatchString(t.Binary):
		return nil, nil, fmt.Errorf("binary %q: not a plain absolute path", t.Binary)
	case !calendarRE.MatchString(t.OnCalendar):
		return nil, nil, fmt.Errorf("schedule %q: invalid characters", t.OnCalendar)
	case t.RandomDelay < 0 || t.RandomDelay > 86400:
		return nil, nil, fmt.Errorf("random delay %d s: must be 0-86400", t.RandomDelay)
	}
	for _, a := range t.Args {
		if !argRE.MatchString(a) {
			return nil, nil, fmt.Errorf("timer argument %q: invalid", a)
		}
	}
	unitHeaders := [][2]string{{"Description", name.Name + " " + t.Description}}
	if t.OnFailure != "" {
		unitHeaders = append(unitHeaders, [2]string{"OnFailure", t.OnFailure})
	}
	service, err = unit{
		{"Unit", unitHeaders},
		{"Service", [][2]string{
			{"Type", "oneshot"},
			{"ExecStart", strings.Join(append([]string{t.Binary}, t.Args...), " ")},
		}},
	}.render()
	if err != nil {
		return nil, nil, err
	}
	timer, err = unit{
		{"Unit", [][2]string{{"Description", name.Name + " schedule: " + t.Description}}},
		{"Timer", [][2]string{
			{"OnCalendar", t.OnCalendar},
			{"RandomizedDelaySec", strconv.Itoa(t.RandomDelay)},
			{"Persistent", "true"}, // runs a missed schedule after downtime
		}},
		{"Install", [][2]string{{"WantedBy", "timers.target"}}},
	}.render()
	return service, timer, err
}

// UnitName is the base name of the timer's units.
func (t Timer) UnitName() string { return name.Unit + t.Name }
