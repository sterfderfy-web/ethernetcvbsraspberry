import base64
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
from urllib.error import HTTPError
from urllib.request import Request, urlopen
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('panel', Path(__file__).with_name('panel.py'))
panel = importlib.util.module_from_spec(spec)
spec.loader.exec_module(panel)


class PanelTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.original_root = panel.ROOT
        panel.ROOT = self.root
        (self.root / 'settings.json').write_text(json.dumps(panel.DEFAULT))
        (self.root / 'image.env').write_text(panel.environment(panel.DEFAULT))
        (self.root / 'panel-password').write_text('test-only-secret')

    def tearDown(self):
        panel.ROOT = self.original_root
        self.directory.cleanup()

    def test_rejects_nonfinite_injection_unknown_and_fractional_controls(self):
        for key, value in [('gamma', float('nan')), ('lift', float('inf')),
                           ('brightness', 1.5), ('contrast', '1; reboot'),
                           ('saturation', True), ('gain', 8)]:
            data = dict(panel.DEFAULT, **{key: value})
            with self.assertRaises(ValueError):
                panel.validate(data)
        with self.assertRaises(ValueError):
            panel.validate(dict(panel.DEFAULT, command='reboot'))

    def test_applies_and_persists_profile(self):
        settings = dict(panel.DEFAULT, gamma=1.8, brightness=75)
        with patch.object(panel.subprocess, 'run') as run:
            panel.apply_settings(settings)
            self.assertEqual(run.call_args_list[0].args[0], ['systemctl', 'restart', 'cvbs-capture'])
        self.assertEqual(panel.read_settings(), settings)
        self.assertIn('gamma=1.8:', (self.root / 'image.env').read_text())
        self.assertIn('brightness=75,', (self.root / 'image.env').read_text())

    def test_failed_restart_restores_both_files(self):
        old = {p.name: p.read_bytes() for p in self.root.iterdir()}
        with patch.object(panel.subprocess, 'run', side_effect=[
                subprocess.CalledProcessError(1, 'systemctl'), None]):
            with self.assertRaises(subprocess.CalledProcessError):
                panel.apply_settings(dict(panel.DEFAULT, gamma=2))
        self.assertEqual((self.root / 'image.env').read_bytes(), old['image.env'])
        self.assertEqual((self.root / 'settings.json').read_bytes(), old['settings.json'])

    def test_migrates_previous_image_profile_without_reset(self):
        import os
        (self.root / 'settings.json').unlink()
        old = panel.environment(dict(panel.DEFAULT, brightness=70, gamma=1.6))
        (self.root / 'image.env').write_text(old)
        directory = str(Path(__file__).parent.resolve())
        script = "import sys,runpy;sys.path.insert(0,sys.argv[1]);runpy.run_path(sys.argv[1]+'/seed-settings.py')"
        subprocess.run([sys.executable, '-c', script, directory], check=True,
                       env=dict(os.environ, CVBS_CONFIG=str(self.root)))
        self.assertEqual(panel.read_settings()['brightness'], 70)
        self.assertEqual(panel.read_settings()['gamma'], 1.6)
        self.assertEqual((self.root / 'image.env').read_text(), old)

    def test_http_auth_csrf_validation_save_and_snapshot(self):
        server = panel.ThreadingHTTPServer(('127.0.0.1', 0), panel.Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        url = 'http://127.0.0.1:' + str(server.server_port)
        auth = 'Basic ' + base64.b64encode(b'admin:test-only-secret').decode()
        try:
            with self.assertRaises(HTTPError) as error:
                urlopen(url + '/api/settings')
            self.assertEqual(error.exception.code, 401)
            request = Request(url + '/api/settings', headers={'Authorization': auth})
            self.assertEqual(json.load(urlopen(request)), panel.DEFAULT)
            body = json.dumps(dict(panel.DEFAULT, gamma=1.7)).encode()
            with self.assertRaises(HTTPError) as error:
                urlopen(Request(url + '/api/settings', body, {'Authorization': auth}))
            self.assertEqual(error.exception.code, 403)
            headers = {'Authorization': auth, 'X-CVBS-Control': '1', 'Content-Type': 'application/json'}
            with patch.object(panel.subprocess, 'run'):
                result = json.load(urlopen(Request(url + '/api/settings', body, headers)))
                self.assertEqual(result['gamma'], 1.7)
            with self.assertRaises(HTTPError) as error:
                urlopen(Request(url + '/api/settings', b'{"command":"reboot"}', headers))
            self.assertEqual(error.exception.code, 400)
            with patch.object(panel.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, b'\xff\xd8test\xff\xd9')):
                response = urlopen(Request(url + '/snapshot.jpg', headers={'Authorization': auth}))
                self.assertEqual(response.headers['Content-Type'], 'image/jpeg')
                self.assertTrue(response.read().startswith(b'\xff\xd8'))
            with patch.object(panel.subprocess, 'run', side_effect=subprocess.TimeoutExpired('ffmpeg', 10)):
                with self.assertRaises(HTTPError) as error:
                    urlopen(Request(url + '/snapshot.jpg', headers={'Authorization': auth}))
                self.assertEqual(error.exception.code, 503)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == '__main__':
    unittest.main()
