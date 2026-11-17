package quadlet

import (
	"bytes"
	"fmt"
	"strings"
)

// iniProperty represents a single key-value entry in a section.
type iniProperty struct {
	Key   string
	Value string
}

// iniSection represents a group of properties under a bracketed header.
type iniSection struct {
	Name       string
	Properties []iniProperty
}

// iniDocument represents an ordered collection of sections.
type iniDocument struct {
	HeaderComment string
	Sections      []iniSection
}

// addSection appends a new section with the given name.
func (d *iniDocument) addSection(name string) *iniSection {
	d.Sections = append(d.Sections, iniSection{Name: name})
	return &d.Sections[len(d.Sections)-1]
}

// add appends a key-value property to the section.
func (s *iniSection) add(key, value string) {
	if value == "" {
		return
	}
	s.Properties = append(s.Properties, iniProperty{Key: key, Value: value})
}

// render formats the INI document into standard systemd unit file syntax.
func (d *iniDocument) render() ([]byte, error) {
	var buf bytes.Buffer
	if d.HeaderComment != "" {
		fmt.Fprintf(&buf, "%s\n", d.HeaderComment)
	}
	for i, s := range d.Sections {
		if strings.ContainsAny(s.Name, "]\r\n\x00") {
			return nil, fmt.Errorf("invalid section name %q", s.Name)
		}
		if i > 0 {
			buf.WriteByte('\n')
		}
		fmt.Fprintf(&buf, "[%s]\n", s.Name)
		for _, p := range s.Properties {
			if strings.ContainsAny(p.Key, "=\r\n\x00") {
				return nil, fmt.Errorf("invalid property key %q", p.Key)
			}
			if strings.ContainsAny(p.Value, "\r\n\x00") {
				return nil, fmt.Errorf("property %q contains newline or null byte", p.Key)
			}
			if strings.HasSuffix(strings.TrimRight(p.Value, " \t"), "\\") {
				return nil, fmt.Errorf("property %q must not end with a backslash", p.Key)
			}
			fmt.Fprintf(&buf, "%s=%s\n", p.Key, p.Value)
		}
	}
	return buf.Bytes(), nil
}
