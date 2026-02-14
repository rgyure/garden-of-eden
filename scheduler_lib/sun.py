from datetime import datetime
from astral import LocationInfo
from astral.sun import sun
import pytz


def get_location(config):
    loc = config['location']
    return LocationInfo(
        name=loc['name'],
        region='',
        timezone=loc['timezone'],
        latitude=loc['latitude'],
        longitude=loc['longitude'],
    )


def get_sun_times(config, date=None):
    """Return sunrise, noon, and sunset for the given date (default: today).

    All returned datetimes are timezone-aware in the configured timezone.
    """
    location = get_location(config)
    tz = pytz.timezone(config['location']['timezone'])

    if date is None:
        date = datetime.now(tz).date()

    s = sun(location.observer, date=date, tzinfo=tz)
    return s['sunrise'], s['noon'], s['sunset']
