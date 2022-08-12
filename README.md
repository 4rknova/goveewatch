# Goveewatch

Simple utility to monitor readings from Govee thermometer / hygrometer devices.

This project is based on
[Thrilleratplay/GoveeWatcher](https://github.com/Thrilleratplay/GoveeWatcher)

# Installation

Install dependencies with:

    pip install -r requirements.txt

To install in your system, copy the script to a suitable directory (eg. /usr/bin)

# Permissions

To run scripts without having to run as root to access the Bluetooth LE adapter
you can use the setcap utility to give the Python3 binary necessary permission,
for example:

    sudo setcap cap_net_raw,cap_net_admin+eip $(eval readlink -f `which python3`)
    
# Configuration

Running the script for the first time will generate an empty JSON configuration 
file in the user's root directory. The filename of the configuration file is
.goveewatch.conf

Here's an example of how to setup aliases for your devices
and set visual thresholds for temperature and humidity:

        {
            "thresholds": { 
                "temp low" : "17",
                "temp high": "20",
                "humidity low" : "50",
                "humidity high": "60"
            },
            "devices":[
                {
                    "name"  : "GVH5075_AAAA",
                    "alias" : "Bedroom"
                },
                {
                    "name"  : "GVH5075_AACC",
                    "alias" : "Bathroom"
                }
            ]
        }


# Sample output

    Mac Address         Location       Temperature  Humidity    Last seen
    A4:C1:38:XX:YY:ZZ   Bedroom        25.34 °      36.40%       2 seconds ago
    A4:C1:38:XX:YY:ZZ   Guest Bedroom  22.24 °      40.10%       4 seconds ago
    A4:C1:38:XX:YY:ZZ   Office         25.84 °      41.20%       2 seconds ago
    A4:C1:38:XX:YY:ZZ   Kitchen        27.75 °      47.80%       2 seconds ago

# References

1. [Bleson manual](https://bleson.readthedocs.io/en/latest/installing.html)
