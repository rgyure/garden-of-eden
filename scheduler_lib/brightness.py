import math
from datetime import datetime
import pytz

from scheduler_lib.sun import get_sun_times


def compute_brightness(now, sunrise, noon, sunset, dawn_brightness, peak_brightness, curve_factor):
    """Compute target brightness using an exponential curve.

    Returns an integer brightness 0-100.
    - Before sunrise or after sunset: 0
    - Sunrise to noon: exponential ramp from dawn_brightness to peak_brightness
    - Noon to sunset: hold at peak_brightness
    """
    if now <= sunrise or now >= sunset:
        return 0

    # After noon, hold at peak
    if now >= noon:
        return int(round(peak_brightness))

    a = curve_factor
    dawn = dawn_brightness
    peak = peak_brightness

    # Ascending: sunrise -> noon
    total = (noon - sunrise).total_seconds()
    elapsed = (now - sunrise).total_seconds()
    progress = elapsed / total if total > 0 else 1.0

    # Exponential curve: f(p) = dawn + (peak - dawn) * (1 - e^(-a*p)) / (1 - e^(-a))
    # Higher curve_factor = faster ramp toward peak (more exponential)
    if a == 0:
        # Linear fallback
        brightness = dawn + (peak - dawn) * progress
    else:
        brightness = dawn + (peak - dawn) * (1 - math.exp(-a * progress)) / (1 - math.exp(-a))

    return int(round(brightness))


def get_target_brightness(config, now=None):
    """Get the target brightness for the current (or given) time.

    Returns an integer 0-100.
    """
    tz = pytz.timezone(config['location']['timezone'])
    if now is None:
        now = datetime.now(tz)
    elif now.tzinfo is None:
        now = tz.localize(now)

    sunrise, noon, sunset = get_sun_times(config, date=now.date())

    light_cfg = config['light']
    return compute_brightness(
        now, sunrise, noon, sunset,
        light_cfg['dawn_brightness'],
        light_cfg['peak_brightness'],
        light_cfg['curve_factor'],
    )
