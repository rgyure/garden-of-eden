"""
DS18B20 1-Wire submersible water temperature probe.

The DS18B20 is the standard sealed digital temp sensor for hydroponic
reservoirs. Hook the data line to GPIO 4 (the default 1-Wire pin),
add a 4.7K pull-up between data and 3.3V, then enable the kernel
overlay:

    /boot/config.txt:  dtoverlay=w1-gpio
    sudo modprobe w1-gpio w1-therm     # (typically auto on next boot)

After that, every probe appears at /sys/bus/w1/devices/28-XXXXXXXX/
and exposes a 'temperature' file that returns millidegrees Celsius.

Two modes:
  - Stub (default, WATER_TEMP_STUB=true): emits a slowly-varying
    value around 21C so the dashboard + EZO temperature compensation
    pathway can be tested before the probe arrives.
  - Real (WATER_TEMP_STUB=false): reads the first DS18B20 device on
    the 1-Wire bus.
"""

import glob
import math
import random
import sys
import os
import time

sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), '../../..')))
import config


W1_DEVICE_GLOB = "/sys/bus/w1/devices/28-*"
W1_TEMPERATURE_FILE = "temperature"


class WaterTempSensor:
    def read(self) -> float:
        raise NotImplementedError


class WaterTempSensorStub(WaterTempSensor):
    """Slowly oscillates around 21C with small noise so the dashboard moves."""
    def __init__(self):
        self._t0 = time.time()

    def read(self) -> float:
        elapsed = time.time() - self._t0
        baseline = 21.0 + 1.5 * math.sin(elapsed / 3600.0)  # ~1 hour period
        noise = random.uniform(-0.1, 0.1)
        return round(baseline + noise, 2)


class WaterTempSensorDS18B20(WaterTempSensor):
    """Real DS18B20 driver via Linux 1-Wire sysfs."""

    def __init__(self, device_path: str = None):
        if device_path is None:
            matches = sorted(glob.glob(W1_DEVICE_GLOB))
            if not matches:
                raise FileNotFoundError(
                    "No DS18B20 device found at /sys/bus/w1/devices/28-*. "
                    "Confirm dtoverlay=w1-gpio in /boot/config.txt and that the "
                    "4.7K pull-up is wired to the data line."
                )
            device_path = matches[0]
        self.device_path = device_path
        self.temperature_file = os.path.join(device_path, W1_TEMPERATURE_FILE)

    def read(self) -> float:
        with open(self.temperature_file, "r") as f:
            milli_c = int(f.read().strip())
        return round(milli_c / 1000.0, 2)


def make_sensor() -> WaterTempSensor:
    if config.WATER_TEMP_STUB:
        return WaterTempSensorStub()
    return WaterTempSensorDS18B20()


try:
    water_temp_sensor = make_sensor()
except Exception as e:
    print(f"WARNING: water-temp sensor unavailable, falling back to stub: {e}")
    water_temp_sensor = WaterTempSensorStub()


if __name__ == "__main__":
    print(f"Water temperature: {water_temp_sensor.read()}C")
