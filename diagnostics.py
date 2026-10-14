"""Bounded, read-only diagnostics; never opens the busy V4L device."""
import json
from pathlib import Path
import subprocess
import time

PROGRESS = Path('/run/cvbs-capture/progress')


def command(args, timeout=5):
    try:
        p = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
        return {'ok': p.returncode == 0, 'text': (p.stdout + p.stderr)[-12000:]}
    except (OSError, subprocess.SubprocessError) as e:
        return {'ok': False, 'text': str(e)}


def status(root):
    services = {}
    for name in ('capture', 'rtsp', 'weak-video', 'reset'):
        result = command(['systemctl', 'show', 'cvbs-' + name,
                          '--property=ActiveState,SubState,NRestarts,Result'])
        services[name] = dict(line.split('=', 1) for line in result['text'].splitlines() if '=' in line)
    progress = {}
    age = None
    try:
        age = max(0, time.time() - PROGRESS.stat().st_mtime)
        # ffmpeg appends progress: read only its most recent report.
        with PROGRESS.open('rb') as stream:
            stream.seek(max(0, PROGRESS.stat().st_size - 4096))
            for line in stream.read().decode(errors='replace').splitlines():
                if '=' in line:
                    key, value = line.split('=', 1)
                    progress[key] = value
    except OSError:
        pass
    device = ''
    try:
        device = (root / 'device.env').read_text().strip().split('CVBS_DEVICE=', 1)[1].splitlines()[0]
    except (OSError, IndexError):
        pass
    return dict(services=services, device=device, device_present=bool(device and Path(device).exists()),
                progress=progress, progress_age_seconds=round(age, 1) if age is not None else None,
                frames_arriving=age is not None and age < 5 and int(progress.get('frame', '0')) > 0 and services['capture'].get('ActiveState') == 'active',
                load_average=Path('/proc/loadavg').read_text().split()[:3],
                logs=command(['journalctl', '-u', 'cvbs-capture', '-u', 'cvbs-weak-video',
                              '-u', 'cvbs-reset', '-n', '35', '--no-pager'])['text'])


def probe():
    result = command(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-rtsp_transport', 'tcp',
                      '-timeout', '5000000', '-i', 'rtsp://127.0.0.1:554/live',
                      '-frames:v', '1', '-f', 'null', '-'], timeout=10)
    return {'image_received': result['ok'], 'detail': result['text'],
            'note': 'Декодирование кадра не подтверждает наличие аналогового сигнала: кадр может быть чёрным или содержать шум.'}
