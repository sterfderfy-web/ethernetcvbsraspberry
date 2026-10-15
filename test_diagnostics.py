import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import types
import unittest
from unittest.mock import patch
from urllib.request import Request, urlopen
from urllib.error import HTTPError

import diagnostics
import panel


class DiagnosticTests(unittest.TestCase):
    def test_status_fresh_stale_and_missing_progress(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            progress = root / 'progress'
            progress.write_text('frame=25\nfps=25.0\nprogress=continue\n')
            def read(path, *args, **kwargs):
                return '0.1 0.2 0.3 1/100 1' if str(path) == '/proc/loadavg' else 'CVBS_DEVICE=' + folder
            with patch.object(diagnostics, 'PROGRESS', progress), \
                 patch.object(Path, 'read_text', read), \
                 patch.object(diagnostics, 'command', return_value={'ok': True, 'text': 'ActiveState=active\nSubState=running'}):
                self.assertTrue(diagnostics.status(root)['frames_arriving'])
                with patch.object(diagnostics.time, 'time', return_value=progress.stat().st_mtime + 20):
                    self.assertFalse(diagnostics.status(root)['frames_arriving'])
                progress.unlink()
                self.assertFalse(diagnostics.status(root)['frames_arriving'])

    def test_probe_success_failure_timeout(self):
        for code in (0, 1):
            with patch.object(subprocess, 'run', return_value=subprocess.CompletedProcess([], code, '', '')):
                self.assertEqual(diagnostics.probe()['image_received'], code == 0)
        with patch.object(subprocess, 'run', side_effect=subprocess.TimeoutExpired('ffmpeg', 10)):
            self.assertFalse(diagnostics.probe()['image_received'])

    def test_reset_requires_header_and_reports_busy_or_failure(self):
        server = panel.ThreadingHTTPServer(('127.0.0.1', 0), panel.Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        url = f'http://127.0.0.1:{server.server_port}/api/restart'
        try:
            with self.assertRaises(HTTPError) as error:
                urlopen(Request(url, method='POST'))
            self.assertEqual(error.exception.code, 403)
            request = Request(url, headers={'X-CVBS-Control': '1'}, method='POST')
            with patch.object(panel, 'reset_busy', return_value=False), patch.object(subprocess, 'run') as run:
                self.assertEqual(urlopen(request).status, 202)
                self.assertEqual(run.call_args.args[0], ['systemctl', 'start', '--no-block', 'cvbs-reset'])
            with patch.object(panel, 'reset_busy', return_value=True):
                with self.assertRaises(HTTPError) as error:
                    urlopen(request)
                self.assertEqual(error.exception.code, 409)
            with patch.object(panel, 'reset_busy', side_effect=OSError('missing systemctl')):
                with self.assertRaises(HTTPError) as error:
                    urlopen(request)
                self.assertEqual(error.exception.code, 503)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

    def test_reset_refuses_unverified_patch_rollback(self):
        # Import Linux-only helper with a fake ioctl on Windows too.
        with patch.dict(sys.modules, {'fcntl': types.SimpleNamespace(ioctl=lambda *a: None)}):
            spec = importlib.util.spec_from_file_location('reset_capture', Path(__file__).with_name('reset-capture.py'))
            reset = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(reset)
        with patch.object(reset, 'usb_device', return_value=Path('/dev/bus/usb/001/002')), \
             patch.object(reset, 'selected_device', return_value='/dev/video0'), \
             patch.object(Path, 'read_text', side_effect=['/tmp/patch', 'restoration failures: [error]']), \
             patch.object(reset, 'run') as run, \
             patch.object(subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)):
            with self.assertRaisesRegex(RuntimeError, 'rollback not verified'):
                reset.main()
            self.assertEqual(run.call_count, 1)
            self.assertEqual(run.call_args.args, ('systemctl', 'stop', 'cvbs-weak-video'))

    def test_reset_restarts_services_even_if_ioctl_fails(self):
        fake = types.SimpleNamespace(ioctl=lambda *a: (_ for _ in ()).throw(OSError('USB error')))
        with patch.dict(sys.modules, {'fcntl': fake}):
            spec = importlib.util.spec_from_file_location('reset_capture', Path(__file__).with_name('reset-capture.py'))
            reset = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(reset)
        with tempfile.TemporaryDirectory() as folder:
            usb = Path(folder) / 'usb'
            usb.touch()
            with patch.object(reset, 'usb_device', return_value=usb), \
                 patch.object(reset, 'selected_device', return_value='/dev/video0'), \
                 patch.object(reset, 'run') as run, \
                 patch.object(subprocess, 'run', side_effect=[subprocess.CompletedProcess([], 3), subprocess.CompletedProcess([], 0)]):
                with self.assertRaisesRegex(OSError, 'USB error'):
                    reset.main()
                self.assertEqual([call.args for call in run.call_args_list][-2:],
                                 [('systemctl', 'start', 'cvbs-capture'), ('systemctl', 'start', 'cvbs-weak-video')])
