import unittest
from datetime import datetime
import pytz

from scheduler_lib.brightness import compute_brightness, get_target_brightness


class TestComputeBrightness(unittest.TestCase):
    """Tests for the exponential brightness curve calculation."""

    def setUp(self):
        self.tz = pytz.timezone('America/Phoenix')
        self.sunrise = self.tz.localize(datetime(2024, 6, 15, 6, 0, 0))
        self.noon = self.tz.localize(datetime(2024, 6, 15, 12, 0, 0))
        self.sunset = self.tz.localize(datetime(2024, 6, 15, 18, 0, 0))
        self.dawn = 30
        self.peak = 90
        self.factor = 2.0

    def test_before_sunrise_returns_zero(self):
        before = self.tz.localize(datetime(2024, 6, 15, 5, 0, 0))
        result = compute_brightness(before, self.sunrise, self.noon, self.sunset,
                                    self.dawn, self.peak, self.factor)
        self.assertEqual(result, 0)

    def test_after_sunset_returns_zero(self):
        after = self.tz.localize(datetime(2024, 6, 15, 19, 0, 0))
        result = compute_brightness(after, self.sunrise, self.noon, self.sunset,
                                    self.dawn, self.peak, self.factor)
        self.assertEqual(result, 0)

    def test_at_sunrise_returns_dawn(self):
        # Just after sunrise
        just_after = self.tz.localize(datetime(2024, 6, 15, 6, 0, 1))
        result = compute_brightness(just_after, self.sunrise, self.noon, self.sunset,
                                    self.dawn, self.peak, self.factor)
        self.assertAlmostEqual(result, self.dawn, delta=1)

    def test_at_noon_returns_peak(self):
        result = compute_brightness(self.noon, self.sunrise, self.noon, self.sunset,
                                    self.dawn, self.peak, self.factor)
        self.assertEqual(result, self.peak)

    def test_afternoon_holds_peak(self):
        """Brightness after noon should hold at peak until sunset."""
        for hour in [13, 14, 15, 16, 17]:
            t = self.tz.localize(datetime(2024, 6, 15, hour, 0, 0))
            b = compute_brightness(t, self.sunrise, self.noon, self.sunset,
                                   self.dawn, self.peak, self.factor)
            self.assertEqual(b, self.peak, f"Should be peak at hour {hour}")

    def test_monotonic_increase_sunrise_to_noon(self):
        """Brightness should increase monotonically from sunrise to noon."""
        prev = 0
        for hour in range(6, 13):
            t = self.tz.localize(datetime(2024, 6, 15, hour, 0, 0))
            if t <= self.sunrise:
                continue
            b = compute_brightness(t, self.sunrise, self.noon, self.sunset,
                                   self.dawn, self.peak, self.factor)
            self.assertGreater(b, prev, f"Brightness should increase at hour {hour}")
            prev = b

    def test_just_before_sunset_returns_peak(self):
        """Brightness just before sunset should still be at peak."""
        before_sunset = self.tz.localize(datetime(2024, 6, 15, 17, 59, 0))
        b = compute_brightness(before_sunset, self.sunrise, self.noon, self.sunset,
                               self.dawn, self.peak, self.factor)
        self.assertEqual(b, self.peak)

    def test_brightness_within_bounds(self):
        """Brightness should always be between 0 and peak."""
        for minute in range(0, 24 * 60, 15):
            hour = minute // 60
            mins = minute % 60
            t = self.tz.localize(datetime(2024, 6, 15, hour, mins, 0))
            b = compute_brightness(t, self.sunrise, self.noon, self.sunset,
                                   self.dawn, self.peak, self.factor)
            self.assertGreaterEqual(b, 0)
            self.assertLessEqual(b, self.peak)

    def test_zero_curve_factor_is_linear(self):
        """With curve_factor=0, brightness should increase linearly."""
        midpoint = self.tz.localize(datetime(2024, 6, 15, 9, 0, 0))  # halfway sunrise->noon
        b = compute_brightness(midpoint, self.sunrise, self.noon, self.sunset,
                               self.dawn, self.peak, 0)
        expected = self.dawn + (self.peak - self.dawn) * 0.5
        self.assertAlmostEqual(b, int(round(expected)), delta=1)

    def test_at_exact_sunrise_returns_zero(self):
        """At the exact sunrise moment, brightness should be 0 (light not yet on)."""
        result = compute_brightness(self.sunrise, self.sunrise, self.noon, self.sunset,
                                    self.dawn, self.peak, self.factor)
        self.assertEqual(result, 0)

    def test_at_exact_sunset_returns_zero(self):
        """At the exact sunset moment, brightness should be 0."""
        result = compute_brightness(self.sunset, self.sunrise, self.noon, self.sunset,
                                    self.dawn, self.peak, self.factor)
        self.assertEqual(result, 0)


class TestGetTargetBrightness(unittest.TestCase):
    """Tests for the high-level get_target_brightness function."""

    def setUp(self):
        self.config = {
            'location': {
                'name': 'Tucson, Arizona',
                'latitude': 32.2226,
                'longitude': -110.9747,
                'timezone': 'America/Phoenix',
            },
            'light': {
                'dawn_brightness': 30,
                'peak_brightness': 90,
                'curve_factor': 2.0,
            },
        }

    def test_midnight_returns_zero(self):
        tz = pytz.timezone('America/Phoenix')
        midnight = tz.localize(datetime(2024, 6, 15, 0, 0, 0))
        result = get_target_brightness(self.config, now=midnight)
        self.assertEqual(result, 0)

    def test_midday_returns_high_brightness(self):
        tz = pytz.timezone('America/Phoenix')
        midday = tz.localize(datetime(2024, 6, 15, 12, 30, 0))
        result = get_target_brightness(self.config, now=midday)
        self.assertGreater(result, 60)

    def test_returns_integer(self):
        tz = pytz.timezone('America/Phoenix')
        t = tz.localize(datetime(2024, 6, 15, 8, 0, 0))
        result = get_target_brightness(self.config, now=t)
        self.assertIsInstance(result, int)


if __name__ == '__main__':
    unittest.main()
