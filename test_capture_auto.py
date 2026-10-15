import subprocess
import os
import sys
import tempfile
import time
from pathlib import Path
import unittest
from unittest.mock import patch
import capture_auto as capture


class CaptureAutoTests(unittest.TestCase):
    @unittest.skipIf(os.name == 'nt', 'Linux pipe selector test')
    def test_watchdog_terminates_a_process_without_frames(self):
        start = time.monotonic()
        capture.stream([sys.executable, '-c', 'import time;time.sleep(20)'], stall_timeout=0.1)
        self.assertLess(time.monotonic() - start, 5)

    @unittest.skipIf(os.name == 'nt', 'Linux pipe selector test')
    def test_real_progress_pipe_is_saved(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(capture, 'RUNTIME', Path(folder)):
            capture.stream([sys.executable, '-c', "import time;print('frame=25\\nfps=25\\nprogress=continue',flush=True);time.sleep(.1)"])
            self.assertIn('frame=25', (Path(folder) / 'progress').read_text())

    def test_prioritizes_mjpeg_then_alternative_formats_and_defaults(self):
        text = """[0]: 'MJPG' (Motion-JPEG)
 Size: Discrete 640x480
 Interval: Discrete 0.040s (25.000 fps)
 Size: Discrete 720x480
 Interval: Discrete 0.040s (25.000 fps)
[1]: 'YUYV' (YUYV 4:2:2)
 Size: Discrete 480x320
 Interval: Discrete 0.040s (25.000 fps)
"""
        modes = capture.modes(text)
        self.assertEqual(modes[0], ('mjpeg', 720, 480, 25.0))
        self.assertEqual(modes[1][0], 'yuyv422')
        self.assertIsNone(modes[-1])

    def test_stepwise_or_unknown_formats_still_try_driver_defaults(self):
        self.assertEqual(capture.modes("[0]: 'XXXX'\nSize: Stepwise 32x32 - 1920x1080"), [None])

    def test_absent_controls_do_not_abort_capture(self):
        with patch.object(capture, 'query', return_value='') as query:
            result = capture.controls('/dev/video0', 'brightness=60,contrast=200')
        self.assertEqual(result, {'applied': {}, 'skipped': ['brightness', 'contrast']})
        self.assertEqual(query.call_count, 1)

    def test_controls_are_clamped_and_failed_control_is_skipped(self):
        with patch.object(capture, 'query', side_effect=[
            ' brightness 0x001 (int) : min=-64 max=64 step=2\n contrast 0x002 (int) : min=0 max=10 step=1',
            '', RuntimeError('unsupported')]) as query:
            result = capture.controls('/dev/video0', 'brightness=255,contrast=200')
        self.assertEqual(result, {'applied': {'brightness': 64}, 'skipped': ['contrast']})
        self.assertEqual(query.call_args_list[1].args[1], '--set-ctrl=brightness=64')

    def test_input_default_does_not_force_mjpeg_or_size(self):
        command = capture.ffmpeg_command('/dev/video7', None, 'format=yuv420p')
        self.assertNotIn('-input_format', command)
        self.assertNotIn('-video_size', command)
        self.assertIn('/dev/video7', command)

    def test_main_tries_next_mode_after_failure(self):
        with patch.object(capture, 'RUNTIME'), patch.object(capture, 'devices', return_value=['/dev/video0']), \
             patch.object(capture, 'query', return_value=''), patch.object(capture, 'controls', return_value={}), \
             patch.object(capture, 'modes', return_value=[('mjpeg',720,480,25),None]), \
             patch.object(capture.Path, 'exists', return_value=True), patch.object(capture, 'save'), \
             patch.object(capture, 'stream') as stream:
            with self.assertRaises(SystemExit):
                capture.main()
        self.assertEqual(stream.call_count, 2)
        self.assertIn('-input_format', stream.call_args_list[0].args[0])
        self.assertNotIn('-input_format', stream.call_args_list[1].args[0])
