package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sort"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/godbus/dbus/v5"
	"github.com/muka/go-bluetooth/api"
	"github.com/muka/go-bluetooth/bluez/profile/adapter"
	"github.com/muka/go-bluetooth/bluez/profile/device"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// ── Config ────────────────────────────────────────────────────────────────────

const goveeConfigFile = ".goveewatch.conf"

// configFile mirrors the YAML on disk exactly.
type configFile struct {
	Features   map[string]string `yaml:"features"`
	Thresholds map[string]string `yaml:"thresholds"`
	Devices    []struct {
		Name  string `yaml:"name"`
		Alias string `yaml:"alias"`
	} `yaml:"devices"`
}

// Config is the parsed, typed representation used by the rest of the program.
type Config struct {
	TempLow      float64
	TempHigh     float64
	HumidLow     float64
	HumidHigh    float64
	BatteryLow   float64
	BlinkWarn    bool
	TempUnit     string            // "C" or "F"
	KnownDevices map[string]string // MAC → alias
}

func configFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, goveeConfigFile)
}

func writeSkeletonConfig(path string) error {
	skeleton := configFile{
		Features: map[string]string{
			"blinking text": "false",
			"temp unit":     "C",
		},
		Thresholds: map[string]string{
			"temp low": "15", "temp high": "30",
			"humidity low": "40", "humidity high": "70",
			"battery low": "10",
		},
		Devices: []struct {
			Name  string `yaml:"name"`
			Alias string `yaml:"alias"`
		}{},
	}
	data, err := yaml.Marshal(skeleton)
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
	if err := yaml.Unmarshal(data, &cf); err != nil {
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
	unit := cf.Features["temp unit"]
	if unit != "C" && unit != "F" {
		unit = "C"
	}
	return Config{
		TempLow:      getF(cf.Thresholds, "temp low",      "15"),
		TempHigh:     getF(cf.Thresholds, "temp high",     "30"),
		HumidLow:     getF(cf.Thresholds, "humidity low",  "40"),
		HumidHigh:    getF(cf.Thresholds, "humidity high", "70"),
		BatteryLow:   getF(cf.Thresholds, "battery low",   "10"),
		BlinkWarn:    cf.Features["blinking text"] == "true",
		TempUnit:     unit,
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

var (
	solBg     = tcell.GetColor("#002b36")
	solBlue   = tcell.GetColor("#268bd2")
	solRed    = tcell.GetColor("#dc322f")
	solTeal   = tcell.GetColor("#2aa198")
	solYellow = tcell.GetColor("#b58900")
	solBase01 = tcell.GetColor("#586e75")
	solBase1  = tcell.GetColor("#93a1a1")
	solBase3  = tcell.GetColor("#eee8d5")
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

	// GetManufacturerData() has a broken type assertion in the library — it
	// asserts map[uint16]interface{} but BlueZ delivers map[uint16]dbus.Variant.
	// Read the property directly to get the real type.
	prop, err := dev.GetProperty("ManufacturerData")
	if err == nil {
		if mfgMap, ok := prop.Value().(map[uint16]dbus.Variant); ok {
			if variant, ok := mfgMap[goveeManufKey]; ok {
				raw, ok := variant.Value().([]byte)
				if ok && len(raw) >= 5 {
					// BlueZ strips the 2-byte company ID from the dict value.
					// Layout: [0]=padding, [1:4]=encoded temp/humidity, [4]=battery
					encoded := int32(raw[1])<<16 | int32(raw[2])<<8 | int32(raw[3])
					encoded = signEncoded(encoded)
					battery := int(raw[4])

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

// ── UI ──────────────────────────────────────────────────────────────────────

func statMtime(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.ModTime().UnixNano(), nil
}

func safeDrawText(s tcell.Screen, row, col int, text string, style tcell.Style) {
	w, h := s.Size()
	if row >= h || col >= w {
		return
	}
	for i, r := range []rune(text) {
		if col+i >= w {
			break
		}
		s.SetContent(col+i, row, r, nil, style)
	}
}

func runMinimalUI(screen tcell.Screen, cfgPath string) {
	cfg, _ := loadConfig(cfgPath)
	cfgMtime, _ := statMtime(cfgPath)

	styleNormal := tcell.StyleDefault
	styleLow    := tcell.StyleDefault.Foreground(tcell.ColorTeal).Bold(true)
	styleHigh   := tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleWarn   := styleHigh
	if cfg.BlinkWarn {
		styleWarn = styleWarn.Blink(true)
	}
	styleBold := tcell.StyleDefault.Bold(true)

	evCh := make(chan tcell.Event, 1)
	go func() {
		for {
			evCh <- screen.PollEvent()
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case ev := <-evCh:
			switch ev := ev.(type) {
			case *tcell.EventKey:
				if (ev.Key() == tcell.KeyRune && ev.Rune() == 'q') || ev.Key() == tcell.KeyCtrlC {
					return
				}
			case *tcell.EventResize:
				screen.Sync()
			}
		case <-ticker.C:
			if mtime, err := statMtime(cfgPath); err == nil && mtime != cfgMtime {
				if newCfg, err := loadConfig(cfgPath); err == nil {
					cfg = newCfg
					cfgMtime = mtime
					styleWarn = styleHigh
					if cfg.BlinkWarn {
						styleWarn = styleWarn.Blink(true)
					}
				}
			}

			screen.Clear()
			row := 0

			safeDrawText(screen, row,  0, "Mac Address", styleBold)
			safeDrawText(screen, row, 20, "Location",    styleBold)
			safeDrawText(screen, row, 42, "Temperature", styleBold)
			safeDrawText(screen, row, 55, "Humidity",    styleBold)
			safeDrawText(screen, row, 65, "Battery",     styleBold)
			safeDrawText(screen, row, 75, "Last seen",   styleBold)
			safeDrawText(screen, row, 90, "Signal",      styleBold)

			devicesMu.RLock()
			snapshot := make([]DeviceData, 0, len(devices))
			for _, d := range devices {
				snapshot = append(snapshot, d)
			}
			devicesMu.RUnlock()
			sort.Slice(snapshot, func(i, j int) bool {
				return snapshot[i].Address < snapshot[j].Address
			})

			for _, d := range snapshot {
				if d.LastSeen.IsZero() {
					continue // RSSI-only entry, no sensor data yet
				}
				row++
				loc := d.Name
				if alias, ok := cfg.KnownDevices[d.Name]; ok {
					loc = alias
				}

				stTemp := styleNormal
				stHum  := styleNormal
				stBat  := styleNormal

				if d.TempC < cfg.TempLow  { stTemp = styleLow  }
				if d.TempC > cfg.TempHigh { stTemp = styleHigh }
				if d.Humidity < cfg.HumidLow  { stHum = styleLow  }
				if d.Humidity > cfg.HumidHigh { stHum = styleHigh }
				if float64(d.Battery) < cfg.BatteryLow { stBat = styleWarn }

				secs := int(time.Since(d.LastSeen).Seconds())
				rssi := "---"
				if d.RSSI != nil {
					rssi = fmt.Sprintf("%4d dBm", *d.RSSI)
				}

				safeDrawText(screen, row,  0, d.Address, styleNormal)
				safeDrawText(screen, row, 20, loc, styleNormal)
				safeDrawText(screen, row, 42, fmt.Sprintf("%.2f \u00b0", d.TempC), stTemp)
				safeDrawText(screen, row, 55, fmt.Sprintf("%.2f%%", d.Humidity), stHum)
				safeDrawText(screen, row, 65, fmt.Sprintf("%3d%%", d.Battery), stBat)
				safeDrawText(screen, row, 75, fmt.Sprintf("%4d seconds ago", secs), styleNormal)
				safeDrawText(screen, row, 90, rssi, styleNormal)
			}

			screen.Show()
		}
	}
}

// dotBar returns a 10-character dot bar: ● for filled, ○ for empty. pct is 0..100.
func dotBar(pct float64) string {
	filled := int(pct/10.0 + 0.5)
	if filled > 10 { filled = 10 }
	if filled < 0  { filled = 0  }
	return strings.Repeat("●", filled) + strings.Repeat("○", 10-filled)
}

// blockBar returns a 10-character block bar: ▓ for filled, ░ for empty. pct is 0..100.
func blockBar(pct float64) string {
	filled := int(pct/10.0 + 0.5)
	if filled > 10 { filled = 10 }
	if filled < 0  { filled = 0  }
	return strings.Repeat("▓", filled) + strings.Repeat("░", 10-filled)
}

// drawCard draws one sensor card starting at (row, col). cardWidth is available columns.
func drawCard(screen tcell.Screen, row, col, cardWidth int,
	d DeviceData, cfg Config, showF bool) {

	accentColor := solBlue
	if d.TempC > cfg.TempHigh || d.TempC < cfg.TempLow {
		accentColor = solRed
	}

	styleBorder   := tcell.StyleDefault.Foreground(accentColor).Background(solBg)
	styleName     := tcell.StyleDefault.Foreground(solBase1).Background(solBg)
	styleTemp     := tcell.StyleDefault.Foreground(solBase3).Background(solBg).Bold(true)
	styleTempWarn := tcell.StyleDefault.Foreground(solRed).Background(solBg).Bold(true)
	styleTempLow  := tcell.StyleDefault.Foreground(solTeal).Background(solBg).Bold(true)
	styleHum      := tcell.StyleDefault.Foreground(solTeal).Background(solBg)
	styleHumWarn  := tcell.StyleDefault.Foreground(solRed).Background(solBg)
	styleBat      := tcell.StyleDefault.Foreground(solYellow).Background(solBg)
	styleBatWarn  := tcell.StyleDefault.Foreground(solRed).Background(solBg)
	if cfg.BlinkWarn {
		styleBatWarn = styleBatWarn.Blink(true)
	}

	loc := d.Name
	if alias, ok := cfg.KnownDevices[d.Name]; ok {
		loc = alias
	}
	locLabel := strings.ToUpper(loc)
	if d.TempC > cfg.TempHigh { locLabel += " ⚠ hot" }
	if d.TempC < cfg.TempLow  { locLabel += " ⚠ cold" }

	stTemp := styleTemp
	if d.TempC > cfg.TempHigh { stTemp = styleTempWarn }
	if d.TempC < cfg.TempLow  { stTemp = styleTempLow  }

	var tempStr string
	if showF {
		tempStr = fmt.Sprintf("%.1f°F", d.TempF)
	} else {
		tempStr = fmt.Sprintf("%.1f°C  %.1f°F", d.TempC, d.TempF)
	}

	stHum := styleHum
	if d.Humidity < cfg.HumidLow || d.Humidity > cfg.HumidHigh { stHum = styleHumWarn }
	humStr := fmt.Sprintf("%s %2.0f%% humidity", dotBar(d.Humidity), d.Humidity)

	stBat := styleBat
	if float64(d.Battery) < cfg.BatteryLow { stBat = styleBatWarn }
	batStr := fmt.Sprintf("%s %2d%% battery", blockBar(float64(d.Battery)), d.Battery)

	// Row 0: border + location label
	screen.SetContent(col, row,   '│', nil, styleBorder)
	screen.SetContent(col, row+1, '│', nil, styleBorder)
	screen.SetContent(col, row+2, '│', nil, styleBorder)
	screen.SetContent(col, row+3, '│', nil, styleBorder)

	safeDrawText(screen, row,   col+2, locLabel, styleName)
	safeDrawText(screen, row+1, col+2, tempStr,  stTemp)
	safeDrawText(screen, row+2, col+2, humStr,   stHum)
	safeDrawText(screen, row+3, col+2, batStr,   stBat)

	_ = cardWidth // reserved for future clipping
}

func runRichUI(screen tcell.Screen, cfgPath string) {
	cfg, _ := loadConfig(cfgPath)
	cfgMtime, _ := statMtime(cfgPath)

	showF := cfg.TempUnit == "F"

	evCh := make(chan tcell.Event, 1)
	go func() {
		for { evCh <- screen.PollEvent() }
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case ev := <-evCh:
			switch ev := ev.(type) {
			case *tcell.EventKey:
				switch {
				case ev.Key() == tcell.KeyRune && ev.Rune() == 'q':
					return
				case ev.Key() == tcell.KeyCtrlC:
					return
				case ev.Key() == tcell.KeyRune && ev.Rune() == 'u':
					showF = !showF
				}
			case *tcell.EventResize:
				screen.Sync()
			}

		case <-ticker.C:
			// Hot-reload config (preserve runtime showF toggle)
			if mtime, err := statMtime(cfgPath); err == nil && mtime != cfgMtime {
				if newCfg, err := loadConfig(cfgPath); err == nil {
					cfg = newCfg
					cfgMtime = mtime
				}
			}

			termW, termH := screen.Size()
			screen.Clear()

			// Collect and sort devices
			devicesMu.RLock()
			snapshot := make([]DeviceData, 0, len(devices))
			for _, d := range devices {
				if !d.LastSeen.IsZero() {
					snapshot = append(snapshot, d)
				}
			}
			devicesMu.RUnlock()
			sort.Slice(snapshot, func(i, j int) bool {
				return snapshot[i].Address < snapshot[j].Address
			})

			// Header
			headerStyle := tcell.StyleDefault.Foreground(solBlue).Background(solBg).Bold(true)
			subStyle    := tcell.StyleDefault.Foreground(solBase01).Background(solBg)
			unitStr := "°C"
			if showF { unitStr = "°F" }
			safeDrawText(screen, 0, 0, "GOVEEWATCH", headerStyle)
			right := fmt.Sprintf("%d sensors · %s · u=toggle", len(snapshot), unitStr)
			rightW := len([]rune(right))
			if rightW < termW {
				safeDrawText(screen, 0, termW-rightW, right, subStyle)
			}

			// Separator
			for x := 0; x < termW; x++ {
				screen.SetContent(x, 1, '─', nil, subStyle)
			}

			// 2-column card grid: 4 rows per card + 1 blank row gap
			const cardGap  = 2
			const cardRows = 5
			cardWidth := (termW - cardGap) / 2

			for i, d := range snapshot {
				col := (i % 2) * (cardWidth + cardGap)
				row := 2 + (i/2)*cardRows
				if row+4 >= termH {
					break
				}
				drawCard(screen, row, col, cardWidth, d, cfg, showF)
			}

			// Status line
			safeDrawText(screen, termH-1, 0, "q · quit", subStyle)

			screen.Show()
		}
	}
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	debug   := false
	minimal := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--debug":
			debug = true
		case "--minimal":
			minimal = true
		}
	}

	if debug {
		log.SetLevel(log.DebugLevel)
		f, err := os.OpenFile("goveewatch.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			log.SetOutput(f)
		}
	} else {
		// Suppress logrus output — muka/go-bluetooth emits warnings that
		// would corrupt the TUI if they reach the terminal.
		log.SetOutput(io.Discard)
	}

	cfgPath := configFilePath()

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		fmt.Println("Configuration file not found")
		fmt.Println("Generating empty configuration file:", cfgPath)
		if err := writeSkeletonConfig(cfgPath); err != nil {
			fmt.Fprintln(os.Stderr, "Error writing config:", err)
			os.Exit(1)
		}
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := startBLEScanner(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "BLE error:", err)
		os.Exit(1)
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
		os.Exit(1)
	}
	if err := screen.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI init error:", err)
		os.Exit(1)
	}
	defer screen.Fini()

	screen.SetStyle(tcell.StyleDefault)
	screen.HideCursor()

	if minimal {
		runMinimalUI(screen, cfgPath)
	} else {
		runRichUI(screen, cfgPath)
	}
}
