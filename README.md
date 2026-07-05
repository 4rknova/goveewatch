# goveewatch

A terminal UI for monitoring Govee H5075 temperature and humidity sensors over
Bluetooth LE. Readings update continuously as advertisements are received.

Based on [Thrilleratplay/GoveeWatcher](https://github.com/Thrilleratplay/GoveeWatcher).

## Requirements

- Linux with BlueZ (bluetoothd running)
- Go 1.21 or later (to build from source)

## Installation

```
make
sudo make install
```

This builds the binary and copies it to `/usr/bin/goveewatch`.

To remove it:

```
sudo make uninstall
```

## Permissions

By default, accessing the Bluetooth LE adapter requires root. To run as a
normal user, grant the binary the necessary capabilities after installing:

```
sudo setcap cap_net_raw,cap_net_admin+eip /usr/bin/goveewatch
```

## Usage

```
goveewatch [--minimal] [--debug]
```

- `--minimal` -- plain text table view instead of the card UI
- `--debug` -- write BlueZ debug output to `goveewatch.log`

### Key bindings

| Key | Action |
|-----|--------|
| `q` | Quit |
| `u` | Toggle between Celsius and Fahrenheit |

## Configuration

On first run, goveewatch writes a skeleton configuration file to
`~/.goveewatch.conf` and exits. Edit it, then run again.

The configuration file is YAML. Changes are picked up automatically while
the program is running -- no restart needed.

### Example

```yaml
features:
    blinking text: "false"
    temp unit: C
    columns: "2"
thresholds:
    temp low: "16.5"
    temp high: "25.5"
    humidity low: "50"
    humidity high: "70"
    battery low: "5"
devices:
    - name: GVH5075_1A2B
      alias: Bedroom
    - name: GVH5075_3C4D
      alias: Kitchen
```

### Features

| Key | Values | Description |
|-----|--------|-------------|
| `blinking text` | `true` / `false` | Blink battery warning when level is low |
| `temp unit` | `C` / `F` | Default temperature unit |
| `columns` | integer >= 1 | Number of sensor cards per row (default: 2) |

### Thresholds

Values outside a threshold are highlighted in the UI. All values are strings.

| Key | Unit | Description |
|-----|------|-------------|
| `temp low` | degrees C | Highlight when temperature falls below this |
| `temp high` | degrees C | Highlight when temperature rises above this |
| `humidity low` | percent | Highlight when humidity falls below this |
| `humidity high` | percent | Highlight when humidity rises above this |
| `battery low` | percent | Highlight when battery falls below this |

### Devices

The `devices` list maps raw BLE device names to human-readable aliases shown
in the UI. The `name` field must match the name the sensor advertises (visible
when running without an alias configured).

```yaml
devices:
    - name: GVH5075_60C4
      alias: Living room
    - name: GVH5075_D727
      alias: Basement
```
