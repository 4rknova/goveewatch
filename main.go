package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/muka/go-bluetooth/api"
	"github.com/muka/go-bluetooth/bluez/profile/adapter"
	"github.com/muka/go-bluetooth/bluez/profile/device"
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

// ── BLE scanner ───────────────────────────────────────────────────────────────

// DeviceData holds the latest sensor reading for a single Govee device.
type DeviceData struct {
	Address  string
	Name     string
	TempC    float64
	TempF    float64
	Humidity float64
	Battery  int
	RSSI     *int
	LastSeen time.Time
}

const goveeOUI = "A4:C1:38"
const goveeManufKey = uint16(0xEC88)

var (
	devices   = map[string]DeviceData{}
	devicesMu sync.RWMutex
)

func startBLEScanner(ctx context.Context) error {
	a, err := adapter.GetDefaultAdapter()
	if err != nil {
		return fmt.Errorf("get adapter: %w", err)
	}

	err = a.FlushDevices()
	if err != nil {
		return fmt.Errorf("flush devices: %w", err)
	}

	discovery, cancel, err := api.Discover(a, nil)
	if err != nil {
		return fmt.Errorf("start discovery: %w", err)
	}

	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-discovery:
				if !ok {
					return
				}
				if ev.Type == adapter.DeviceRemoved {
					continue
				}
				handleAdvertisement(ev.Path)
			}
		}
	}()
	return nil
}

func handleAdvertisement(path dbus.ObjectPath) {
	dev, err := device.NewDevice1(path)
	if err != nil {
		return
	}

	addr, err := dev.GetAddress()
	if err != nil || !strings.HasPrefix(addr, goveeOUI) {
		return
	}

	mfgData, err := dev.GetManufacturerData()
	if err == nil {
		if rawIface, ok := mfgData[goveeManufKey]; ok {
			raw, ok := rawIface.([]byte)
			if ok && len(raw) >= 7 {
				// bytes [3:6] are the 3 encoded data bytes; byte [6] is battery
				encoded := int32(raw[3])<<16 | int32(raw[4])<<8 | int32(raw[5])
				encoded = signEncoded(encoded)
				battery := int(raw[6])

				name, _ := dev.GetName()
				name = strings.Split(name, "'")[0]

				devicesMu.Lock()
				d := devices[addr]
				d.Address = addr
				d.Name = name
				d.TempC = decodeTempC(encoded)
				d.TempF = decodeTempF(encoded)
				d.Humidity = decodeHumidity(encoded)
				d.Battery = battery
				d.LastSeen = time.Now()
				devices[addr] = d
				devicesMu.Unlock()
			}
		}
	}

	rssi, err := dev.GetRSSI()
	if err == nil && rssi != 0 {
		r := int(rssi)
		devicesMu.Lock()
		d := devices[addr]
		d.RSSI = &r
		devices[addr] = d
		devicesMu.Unlock()
	}
}

// ── Entry point (stub — full implementation added in Task 6) ──────────────────

func main() {}
