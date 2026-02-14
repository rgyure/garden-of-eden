from datetime import timedelta, datetime
import pytz

from app.scheduler.sun import get_sun_times


def compute_pump_times(sunrise, sunset, runs_per_day):
    """Compute evenly-spaced pump start times during daylight hours.

    Divides the daylight period into (runs_per_day + 1) equal segments
    and places runs at the interior division points.

    Returns a list of datetime objects.
    """
    if runs_per_day <= 0:
        return []

    daylight_seconds = (sunset - sunrise).total_seconds()
    segment = daylight_seconds / (runs_per_day + 1)

    times = []
    for i in range(1, runs_per_day + 1):
        run_time = sunrise + timedelta(seconds=segment * i)
        times.append(run_time)

    return times


def should_pump_be_on(config, now=None):
    """Determine if the pump should be running at the given time.

    Returns (bool, int) - (should_be_on, speed).
    """
    tz = pytz.timezone(config['location']['timezone'])
    if now is None:
        now = datetime.now(tz)
    elif now.tzinfo is None:
        now = tz.localize(now)

    sunrise, noon, sunset = get_sun_times(config, date=now.date())

    pump_cfg = config['pump']
    runs_per_day = pump_cfg['runs_per_day']
    duration = timedelta(minutes=pump_cfg['run_duration_minutes'])
    speed = pump_cfg['speed']

    pump_times = compute_pump_times(sunrise, sunset, runs_per_day)

    for start in pump_times:
        end = start + duration
        if start <= now < end:
            return True, speed

    return False, 0
