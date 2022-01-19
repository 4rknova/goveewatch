#!/usr/bin/env python3

from time import sleep
from os.path import expanduser
import os
import sys
from datetime import datetime
import curses
from bleson import get_provider, Observer, UUID16
from bleson.logger import log, set_level, ERROR, DEBUG
import json

home = expanduser("~")
f_config = home + "/.goveewatch.conf"

# Disable warnings
set_level(ERROR)

# # Uncomment for debug log level
# set_level(DEBUG)

# https://macaddresschanger.com/bluetooth-mac-lookup/A4%3AC1%3A38
# OUI Prefix	Company
# A4:C1:38	Telink Semiconductor (Taipei) Co. Ltd.
GOVEE_BT_mac_OUI_PREFIX = "A4:C1:38"

H5075_UPDATE_UUID16 = UUID16(0xEC88)

govee_devices = {}
known_devices = {}

# ###########################################################################
FORMAT_PRECISION = ".2f"


# Decode H5075 Temperature into degrees Celcius
def decode_temp_in_c(encoded_data):
    return format((encoded_data / 10000), FORMAT_PRECISION)


# Decode H5075 Temperature into degrees Fahrenheit
def decode_temp_in_f(encoded_data):
    return format((((encoded_data / 10000) * 1.8) + 32), FORMAT_PRECISION)


# Decode H5075 percent humidity
def decode_humidity(encoded_data):
    return format(((encoded_data % 1000) / 10), FORMAT_PRECISION)


def print_values(mac):
    govee_device = govee_devices[mac]
    print(
        f"{govee_device['name']} ({govee_device['address']}) - \
Temperature {govee_device['tempInC']}C / {govee_device['tempInF']}F  - \
Humidity: {govee_device['humidity']}% - \
Battery:  {govee_device['battery']}%"
    )


def print_rssi(mac):
    govee_device = govee_devices[mac]
    print(
        f"{govee_device['name']} ({govee_device['address']}) - \
RSSI: {govee_device['rssi']}"
    )


# On BLE advertisement callback
def on_advertisement(advertisement):
    log.debug(advertisement)

    if advertisement.address.address.startswith(GOVEE_BT_mac_OUI_PREFIX):
        mac = advertisement.address.address
        if mac not in govee_devices:
            govee_devices[mac] = {}
    
        if H5075_UPDATE_UUID16 in advertisement.uuid16s:
            # HACK:  Proper decoding is done in bleson > 0.10
            name = advertisement.name.split("'")[0]
            encoded_data = int(advertisement.mfg_data.hex()[6:12], 16)
            battery = int(advertisement.mfg_data.hex()[12:14], 16)
            govee_devices[mac]["address"] = mac
            govee_devices[mac]["name"] = name
            govee_devices[mac]["mfg_data"] = advertisement.mfg_data
            govee_devices[mac]["data"] = encoded_data

            govee_devices[mac]["tempInC"] = decode_temp_in_c(encoded_data)
            govee_devices[mac]["tempInF"] = decode_temp_in_f(encoded_data)
            govee_devices[mac]["humidity"] = decode_humidity(encoded_data)

            govee_devices[mac]["battery"] = battery

            now = datetime.now()
            govee_devices[mac]["timestamp"] = now.strftime("%H:%M:%S")
            # print_values(mac)

        if advertisement.rssi is not None and advertisement.rssi != 0:
            govee_devices[mac]["rssi"] = advertisement.rssi
            # print_rssi(mac)

        log.debug(govee_devices[mac])


# ###########################################################################

if not os.path.isfile(f_config):
    print("Configuration file not found")
    print("Generating empty configuration file: " + f_config)
    text_file = open(f_config, "w")
    text_file.write('{')
    text_file.write('\n\t\"devices\":[')
    text_file.write('\n\t]')
    text_file.write('\n}')
    text_file.close()
    exit(1)
with open(f_config) as conf:
    print("Loading configuration file: " + f_config)
    data = json.load(conf)
    for entry in data["devices"]:
        known_devices[entry["name"]] = entry["alias"] 

adapter = get_provider().get_adapter()
observer = Observer(adapter)
observer.on_advertising_data = on_advertisement

stdscr = curses.initscr()
rows, columns = stdscr.getmaxyx()

curses.curs_set(0)
curses.noecho()

try:
    while True:
        observer.start()
        sleep(2)
        observer.stop()

        rows_new, columns_new = stdscr.getmaxyx()

        if rows_new != rows or columns_new != columns:
            stdscr.clear()

        rows = rows_new
        columns = columns_new

        line = 0
        for key, value in govee_devices.items():
            stdscr.addstr(line, 0, key)
            if value["name"] in known_devices.keys():
                stdscr.addstr(line,20, known_devices[value["name"]])
            else:
                stdscr.addstr(line,20, value["name"])

            stdscr.addstr(line,42, value["tempInC"] + " \u00b0")
            stdscr.addstr(line,55, value["humidity"] + '%')
            stdscr.addstr(line,65, value["timestamp"])
            line = line + 1
        stdscr.refresh()

except KeyboardInterrupt:
    try:
        observer.stop()
        sys.exit(0)
    except SystemExit:
        observer.stop()
        os._exit(0)
