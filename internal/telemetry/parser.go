package telemetry

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

type podmanStatEntry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CPUTime    string `json:"cpu_time"`
	CPUPercent string `json:"cpu_percent"`
	MemUsage   string `json:"mem_usage"`
	MemPercent string `json:"mem_percent"`
	NetIO      string `json:"net_io"`
	BlockIO    string `json:"block_io"`
	PIDs       string `json:"pids"`
}

// ParseHumanBytes converts strings like "103.3MB", "32.65kB", "1.5GiB", "0B" into int64 bytes.
func ParseHumanBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" || s == "0" {
		return 0
	}

	// Separate numeric value and unit
	var numStr strings.Builder
	var unitStr strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			if unitStr.Len() == 0 {
				numStr.WriteRune(r)
			}
		} else {
			unitStr.WriteRune(r)
		}
	}

	val, err := strconv.ParseFloat(numStr.String(), 64)
	if err != nil {
		return 0
	}

	unit := strings.TrimSpace(strings.ToLower(unitStr.String()))
	var mult float64 = 1
	switch unit {
	case "b", "":
		mult = 1
	case "k", "kb", "kib":
		mult = 1024
	case "m", "mb", "mib":
		mult = 1024 * 1024
	case "g", "gb", "gib":
		mult = 1024 * 1024 * 1024
	case "t", "tb", "tib":
		mult = 1024 * 1024 * 1024 * 1024
	}

	return int64(math.Round(val * mult))
}

// ParseSlashPair splits "103.3MB / 536.9MB" and returns (103.3MB, 536.9MB) as bytes.
func ParseSlashPair(s string) (int64, int64) {
	parts := strings.Split(s, "/")
	if len(parts) == 0 {
		return 0, 0
	}
	first := ParseHumanBytes(parts[0])
	if len(parts) == 1 {
		return first, 0
	}
	second := ParseHumanBytes(parts[1])
	return first, second
}

// ParsePercent parses strings like "0.01%" or "19.24%" to float64.
func ParsePercent(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return val
}

// ParsePodmanStatsJSON parses the JSON array output of `podman stats --no-stream --format json`.
func ParsePodmanStatsJSON(data []byte, ts time.Time) ([]ContainerMetric, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var entries []podmanStatEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}

	if ts.IsZero() {
		ts = time.Now()
	}

	metrics := make([]ContainerMetric, 0, len(entries))
	for _, e := range entries {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		memUsed, memLimit := ParseSlashPair(e.MemUsage)
		netIn, netOut := ParseSlashPair(e.NetIO)
		blockIn, blockOut := ParseSlashPair(e.BlockIO)

		metrics = append(metrics, ContainerMetric{
			Timestamp:  ts,
			Container:  name,
			CPUPercent: ParsePercent(e.CPUPercent),
			MemBytes:   memUsed,
			MemLimit:   memLimit,
			NetIn:      netIn,
			NetOut:     netOut,
			BlockIn:    blockIn,
			BlockOut:   blockOut,
		})
	}
	return metrics, nil
}
