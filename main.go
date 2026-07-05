package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// ── Config ────────────────────────────────────────────────────────────────────

const configPath = ".goveewatch.conf"

// configFile mirrors the JSON on disk exactly.
type configFile struct {
	Features   map[string]string `json:"features"`
	Thresholds map[string]string `json:"thresholds"`
	Devices    []struct {
		Name  string `json:"name"`
		Alias string `json:"alias"`
	} `json:"devices"`
}

// Config is the parsed, typed representation used by the rest of the program.
type Config struct {
	TempLow      float64
	TempHigh     float64
	HumidLow     float64
	HumidHigh    float64
	BatteryLow   float64
	BlinkWarn    bool
	KnownDevices map[string]string // MAC → alias
}

func configFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".goveewatch.conf")
}

func writeSkeletonConfig(path string) error {
	skeleton := configFile{
		Features: map[string]string{"blinking text": "false"},
		Thresholds: map[string]string{
			"temp low": "15", "temp high": "30",
			"humidity low": "40", "humidity high": "70",
			"battery low": "10",
		},
		Devices: []struct {
			Name  string `json:"name"`
			Alias string `json:"alias"`
		}{},
	}
	data, err := json.MarshalIndent(skeleton, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cf configFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return Config{}, err
	}
	getF := func(m map[string]string, key, def string) float64 {
		v, ok := m[key]
		if !ok {
			v = def
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			f, _ = strconv.ParseFloat(def, 64)
		}
		return f
	}
	known := map[string]string{}
	for _, d := range cf.Devices {
		known[d.Name] = d.Alias
	}
	return Config{
		TempLow:      getF(cf.Thresholds, "temp low", "15"),
		TempHigh:     getF(cf.Thresholds, "temp high", "30"),
		HumidLow:     getF(cf.Thresholds, "humidity low", "40"),
		HumidHigh:    getF(cf.Thresholds, "humidity high", "70"),
		BatteryLow:   getF(cf.Thresholds, "battery low", "10"),
		BlinkWarn:    cf.Features["blinking text"] == "true",
		KnownDevices: known,
	}, nil
}

// ── Decode functions ──────────────────────────────────────────────────────────

// Two's complement for 3-byte signed integer.
// Govee encodes temperature as a 24-bit value; negative temps set the high bit.
func signEncoded(raw int32) int32 {
	if raw > 0x7FFFFF {
		return raw - 0x1000000
	}
	return raw
}

func decodeTempC(encoded int32) float64 {
	return float64(encoded) / 10000.0
}

func decodeTempF(encoded int32) float64 {
	return decodeTempC(encoded)*1.8 + 32
}

func decodeHumidity(encoded int32) float64 {
	return float64(encoded%1000) / 10.0
}

// ── Entry point (stub — full implementation added in Task 6) ──────────────────

func main() {}
