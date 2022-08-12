# Goveewatch

Simple utility to monitor readings from Govee thermometer
hygrometer devices. This project is a fork of 
[Thrilleratplay/GoveWatcher](https://github.com/Thrilleratplay/GoveeWatcher)


Install dependencies with:

    pip install -r requirements.txt

Running the script for the first time will generate an empty
JSON configuration file in the user's root directory. The
filename of the configuration file is .goveewatch.conf

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


To run scripts without having to run as root to access the Bluetooth
LE adapter you can use the setcap utility to give the Python3 binary
necessary permission, for example:

    sudo setcap cap_net_raw,cap_net_admin+eip $(eval readlink -f `which python3`)
    

# References
1. [Bleson manual](https://bleson.readthedocs.io/en/latest/installing.html)
