#!/usr/bin/env python3
"""Local image control panel; no third party Python dependencies."""
import diagnostics
import json
import math
import os
from pathlib import Path
import subprocess
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT = Path(os.environ.get('CVBS_CONFIG', '/etc/cvbs-camera'))
LOCK = threading.Lock()
DEFAULT = dict(brightness=60, contrast=200, saturation=160,
               lift=0.0, gain=1.05, color=1.10, gamma=1.30,
               gamma_weight=0.60, denoise=1.2, sharpness=0.4)
RANGES = dict(brightness=(0, 255), contrast=(0, 255), saturation=(0, 255),
              lift=(-0.3, 0.3), gain=(0.5, 2.5), color=(0, 3), gamma=(0.5, 3),
              gamma_weight=(0, 1), denoise=(0, 6), sharpness=(0, 1.5))


def validate(data):
    if not isinstance(data, dict) or set(data) != set(DEFAULT):
        raise ValueError('Нужен полный набор параметров')
    result = {}
    for key, value in data.items():
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise ValueError('Недопустимое значение: ' + key)
        low, high = RANGES[key]
        if not math.isfinite(value) or not low <= value <= high:
            raise ValueError('Вне диапазона: ' + key)
        if key in ('brightness', 'contrast', 'saturation') and int(value) != value:
            raise ValueError('Нужно целое число: ' + key)
        result[key] = value
    return result


def environment(s):
    controls = ','.join(f'{key}={int(s[key])}' for key in ('brightness', 'contrast', 'saturation'))
    d = s['denoise']
    filters = ('format=yuv420p,'
               f"eq=brightness={s['lift']}:contrast={s['gain']}:saturation={s['color']}:"
               f"gamma={s['gamma']}:gamma_weight={s['gamma_weight']},"
               f"hqdn3d={d}:{d * 5/6}:{d * 1.5}:{d * 1.25},"
               f"unsharp=5:5:{s['sharpness']}:3:3:0")
    return f'CVBS_CAPTURE_CONTROLS={controls}\nCVBS_VIDEO_FILTER={filters}\n'


def atomic(path, content):
    fd, name = tempfile.mkstemp(dir=path.parent)
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(name, 0o644)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def read_settings():
    return validate(json.loads((ROOT / 'settings.json').read_text()))


def apply_settings(s):
    s = validate(s)
    old_env = (ROOT / 'image.env').read_text()
    old_json = (ROOT / 'settings.json').read_text()
    try:
        atomic(ROOT / 'image.env', environment(s))
        subprocess.run(['systemctl', 'restart', 'cvbs-capture'], check=True, timeout=20,
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        subprocess.run(['systemctl', 'is-active', '--quiet', 'cvbs-capture'], check=True, timeout=5)
        atomic(ROOT / 'settings.json', json.dumps(s))
    except Exception:
        atomic(ROOT / 'image.env', old_env)
        atomic(ROOT / 'settings.json', old_json)
        subprocess.run(['systemctl', 'restart', 'cvbs-capture'], timeout=20, check=False)
        raise


def reset_busy():
    state = subprocess.run(['systemctl', 'show', 'cvbs-reset', '--property=ActiveState', '--value'],
                           check=True, capture_output=True, text=True, timeout=5)
    return state.stdout.strip() in ('activating', 'active', 'deactivating')


class Handler(BaseHTTPRequestHandler):
    def setup(self):
        super().setup()
        self.connection.settimeout(30)

    def reply(self, code, body, kind='application/json'):
        if isinstance(body, str):
            body = body.encode('utf-8')
        self.send_response(code)
        self.send_header('Content-Type', kind + ('; charset=utf-8' if kind.startswith('text/') else ''))
        self.send_header('Content-Length', str(len(body)))
        self.send_header('Cache-Control', 'no-store')
        self.send_header('X-Content-Type-Options', 'nosniff')
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split('?')[0]
        try:
            if path == '/':
                self.reply(200, Path(__file__).with_name('index.html').read_bytes(), 'text/html')
            elif path == '/api/status':
                self.reply(200, json.dumps(diagnostics.status(ROOT)))
            elif path == '/api/probe':
                with LOCK:
                    self.reply(200, json.dumps(diagnostics.probe()))
            elif path == '/api/settings':
                with LOCK:
                    self.reply(200, json.dumps(read_settings()))
            elif path == '/snapshot.jpg':
                with LOCK:
                    frame = subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error',
                        '-rtsp_transport', 'tcp', '-timeout', '5000000', '-i',
                        'rtsp://127.0.0.1:554/live', '-frames:v', '1', '-f', 'image2pipe',
                        '-vcodec', 'mjpeg', 'pipe:1'], capture_output=True, timeout=10, check=True)
                    if not frame.stdout:
                        raise RuntimeError('Нет кадра')
                    self.reply(200, frame.stdout, 'image/jpeg')
            else:
                self.reply(404, '{}')
        except (subprocess.SubprocessError, OSError, ValueError, RuntimeError):
            self.reply(503, json.dumps({'error': 'Кадр или настройки временно недоступны'}, ensure_ascii=False))

    def do_POST(self):
        # Custom header disallows cross-origin forms; JSON fetch triggers a preflight.
        if self.headers.get('X-CVBS-Control') != '1':
            self.reply(403, '{}')
            return
        if self.path == '/api/restart':
            try:
                with LOCK:
                    if reset_busy():
                        self.reply(409, json.dumps({'error': 'USB reset already running'}))
                        return
                    subprocess.run(['systemctl', 'start', '--no-block', 'cvbs-reset'],
                                   check=True, capture_output=True, timeout=5)
                self.reply(202, json.dumps({'message': 'USB reset queued'}))
            except (OSError, subprocess.SubprocessError):
                self.reply(503, json.dumps({'error': 'Cannot start USB reset'}))
            return
        if self.path != '/api/settings':
            self.reply(404, '{}')
            return
        try:
            length = int(self.headers.get('Content-Length', '0'))
            if not 0 < length <= 4096:
                raise ValueError('Недопустимый размер запроса')
            settings = validate(json.loads(self.rfile.read(length)))
            with LOCK:
                if reset_busy():
                    self.reply(409, json.dumps({'error': 'USB reset running; retry settings later'}))
                    return
                apply_settings(settings)
            self.reply(200, json.dumps(settings))
        except (ValueError, UnicodeError) as error:
            self.reply(400, json.dumps({'error': str(error)}, ensure_ascii=False))
        except (subprocess.SubprocessError, OSError):
            self.reply(503, json.dumps({'error': 'Не удалось применить; восстановлены предыдущие настройки'}, ensure_ascii=False))


if __name__ == '__main__':
    ThreadingHTTPServer(('0.0.0.0', 8080), Handler).serve_forever()
