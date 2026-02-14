import unittest
from datetime import datetime, timedelta
import pytz

from scheduler_lib.pump_schedule import compute_pump_times, should_pump_be_on


class TestComputePumpTimes(unittest.TestCase):
    """Tests for evenly-spaced pump run timing."""

    def setUp(self):
        self.tz = pytz.timezone('America/Phoenix')
        self.sunrise = self.tz.localize(datetime(2024, 6, 15, 6, 0, 0))
        self.sunset = self.tz.localize(datetime(2024, 6, 15, 18, 0, 0))

    def test_four_runs_produces_four_times(self):
        times = compute_pump_times(self.sunrise, self.sunset, 4)
        self.assertEqual(len(times), 4)

    def test_zero_runs_returns_empty(self):
        times = compute_pump_times(self.sunrise, self.sunset, 0)
        self.assertEqual(len(times), 0)

    def test_times_are_within_daylight(self):
        times = compute_pump_times(self.sunrise, self.sunset, 4)
        for t in times:
            self.assertGreater(t, self.sunrise)
            self.assertLess(t, self.sunset)

    def test_times_are_evenly_spaced(self):
        times = compute_pump_times(self.sunrise, self.sunset, 4)
        gaps = []
        all_points = [self.sunrise] + times + [self.sunset]
        for i in range(1, len(all_points)):
            gap = (all_points[i] - all_points[i - 1]).total_seconds()
            gaps.append(gap)

        # All gaps should be equal
        for gap in gaps:
            self.assertAlmostEqual(gap, gaps[0], delta=1)

    def test_single_run_at_midpoint(self):
        times = compute_pump_times(self.sunrise, self.sunset, 1)
        self.assertEqual(len(times), 1)
        expected_midpoint = self.sunrise + (self.sunset - self.sunrise) / 2
        self.assertAlmostEqual(
            times[0].timestamp(), expected_midpoint.timestamp(), delta=1
        )

    def test_times_are_sorted(self):
        times = compute_pump_times(self.sunrise, self.sunset, 6)
        for i in range(1, len(times)):
            self.assertGreater(times[i], times[i - 1])


class TestShouldPumpBeOn(unittest.TestCase):
    """Tests for the should_pump_be_on function."""

    def setUp(self):
        self.config = {
            'location': {
                'name': 'Tucson, Arizona',
                'latitude': 32.2226,
                'longitude': -110.9747,
                'timezone': 'America/Phoenix',
            },
            'pump': {
                'runs_per_day': 4,
                'run_duration_minutes': 5,
                'speed': 100,
            },
        }

    def test_midnight_pump_off(self):
        tz = pytz.timezone('America/Phoenix')
        midnight = tz.localize(datetime(2024, 6, 15, 0, 0, 0))
        on, speed = should_pump_be_on(self.config, now=midnight)
        self.assertFalse(on)
        self.assertEqual(speed, 0)

    def test_during_pump_run_returns_on(self):
        """Pump should be on during a scheduled run window."""
        tz = pytz.timezone('America/Phoenix')

        # Calculate when the first pump run starts
        from scheduler_lib.sun import get_sun_times
        sunrise, noon, sunset = get_sun_times(self.config, date=datetime(2024, 6, 15).date())
        times = compute_pump_times(sunrise, sunset, 4)

        # Check 1 minute into the first run
        during_run = times[0] + timedelta(minutes=1)
        on, speed = should_pump_be_on(self.config, now=during_run)
        self.assertTrue(on)
        self.assertEqual(speed, 100)

    def test_just_after_pump_run_returns_off(self):
        """Pump should be off just after a run window ends."""
        tz = pytz.timezone('America/Phoenix')

        from scheduler_lib.sun import get_sun_times
        sunrise, noon, sunset = get_sun_times(self.config, date=datetime(2024, 6, 15).date())
        times = compute_pump_times(sunrise, sunset, 4)

        # Check 1 second after the first run ends
        after_run = times[0] + timedelta(minutes=5, seconds=1)
        on, speed = should_pump_be_on(self.config, now=after_run)
        self.assertFalse(on)

    def test_returns_configured_speed(self):
        self.config['pump']['speed'] = 75
        tz = pytz.timezone('America/Phoenix')

        from scheduler_lib.sun import get_sun_times
        sunrise, noon, sunset = get_sun_times(self.config, date=datetime(2024, 6, 15).date())
        times = compute_pump_times(sunrise, sunset, 4)

        during_run = times[0] + timedelta(minutes=1)
        on, speed = should_pump_be_on(self.config, now=during_run)
        self.assertTrue(on)
        self.assertEqual(speed, 75)


if __name__ == '__main__':
    unittest.main()
