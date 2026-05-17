#!/usr/bin/env python3
"""
Interactive calibration helper for the Atlas Scientific EZO-pH and EZO-EC
circuits. Run from the project root after activating the venv:

    source venv/bin/activate
    python bin/calibrate-ezo.py ph
    python bin/calibrate-ezo.py ec

Before each prompt, rinse the probe with distilled water, blot dry, and place
it in the relevant buffer. Calibration is persistent in the EZO circuit
firmware - you only need to redo it ~every 3 months or after probe replacement.
"""

import argparse
import sys
import time

sys.path.insert(0, '.')


def calibrate_ph():
    from app.sensors.ph.ph import PHSensorEZO
    s = PHSensorEZO()
    print("EZO-pH found at 0x{:02X}".format(s.address))
    print(s._device.info())
    print("Current calibration:", s.calibration_status())

    for point, value, prompt in [
        ("mid",  7.00,  "Place probe in pH 7.00 buffer, swirl 10s, then press Enter"),
        ("low",  4.00,  "Place probe in pH 4.00 buffer, swirl 10s, then press Enter"),
        ("high", 10.00, "Place probe in pH 10.00 buffer, swirl 10s, then press Enter"),
    ]:
        ans = input(f"\n[{point}] {prompt} (or 's' to skip): ").strip().lower()
        if ans == "s":
            continue
        print("Sampling for 30s...")
        for i in range(6):
            print(f"  reading: {s.read():.2f} pH")
            time.sleep(5)
        print(s.calibrate(point, value))

    print("\nFinal calibration:", s.calibration_status())
    print("Final reading:    ", s.read(), "pH")


def calibrate_ec():
    from app.sensors.ec.ec import ECSensorEZO
    s = ECSensorEZO()
    print("EZO-EC found at 0x{:02X}".format(s.address))
    print(s._device.info())
    print("Current calibration:", s.calibration_status())

    ans = input("\nProbe must be COMPLETELY DRY for dry calibration. Ready? (y/N): ").strip().lower()
    if ans == "y":
        print(s.calibrate_dry())

    ans = input("\nPlace probe in 1413 uS/cm calibration solution. Ready? (y/N): ").strip().lower()
    if ans == "y":
        print("Sampling for 30s...")
        for i in range(6):
            print(f"  reading: {s.read()}")
            time.sleep(5)
        print(s.calibrate_single(1413))

    ans = input("\nTwo-point: place probe in 12880 uS/cm solution. Skip? (y to skip): ").strip().lower()
    if ans != "y":
        print(s.calibrate_high(12880))

    print("\nFinal calibration:", s.calibration_status())
    print("Final reading:    ", s.read())


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("sensor", choices=["ph", "ec"])
    args = parser.parse_args()
    if args.sensor == "ph":
        calibrate_ph()
    else:
        calibrate_ec()


if __name__ == "__main__":
    main()
