"""Discover USB V4L2 capture devices, negotiate modes and recover failed streams."""
import json
import os
from pathlib import Path
import re
import selectors
import subprocess
import sys
import time

RUNTIME = Path('/run/cvbs-capture')
SYS_VIDEO = Path('/sys/class/video4linux')
FORMATS = {'MJPG': 'mjpeg', 'JPEG': 'mjpeg', 'YUYV': 'yuyv422',
           'UYVY': 'uyvy422', 'NV12': 'nv12', 'YU12': 'yuv420p',
           'RGB3': 'rgb24', 'BGR3': 'bgr24', 'H264': 'h264'}


def log(message):
    print(message, flush=True)


def query(device, option):
    p = subprocess.run(['v4l2-ctl', '-d', str(device), option],
                       capture_output=True, text=True, timeout=8)
    if p.returncode:
        raise RuntimeError(p.stderr.strip() or p.stdout.strip())
    return p.stdout


def usb_parent(video):
    node = Path(video).resolve(strict=True)
    if not re.fullmatch(r'video\d+', node.name):
        raise RuntimeError('Not a V4L2 video node')
    path = (SYS_VIDEO / node.name / 'device').resolve(strict=True)
    for parent in (path, *path.parents):
        if (parent / 'idVendor').exists() and (parent / 'idProduct').exists():
            return parent
    raise RuntimeError('Video device has no USB parent')


def devices(preferred='auto'):
    found = []
    for entry in sorted(SYS_VIDEO.glob('video*'), key=lambda p: int(p.name[5:])):
        device = Path('/dev') / entry.name
        try:
            usb_parent(device)
            info = query(device, '--all')
            caps = info.split('Device Caps', 1)[-1].split('Media Driver', 1)[0]
            if 'Video Capture' in caps and 'Streaming' in caps:
                found.append(str(device))
        except (OSError, RuntimeError, subprocess.SubprocessError) as error:
            log(f'Skip {device}: {error}')
    if preferred != 'auto':
        preferred = str(Path(preferred).resolve())
        if preferred in found:
            found.remove(preferred)
            found.insert(0, preferred)
    return found


def modes(text):
    result = []
    fmt = None
    size = None
    for line in text.splitlines():
        match = re.search(r"\[\d+\]: '(.{4})'", line)
        if match:
            fmt, size = FORMATS.get(match[1]), None
        match = re.search(r'Size: Discrete (\d+)x(\d+)', line)
        if match:
            size = (int(match[1]), int(match[2]))
        match = re.search(r'\(([\d.]+) fps\)', line)
        if match and fmt and size:
            item = (fmt, *size, float(match[1]))
            if item not in result:
                result.append(item)
    result.sort(key=lambda m: (m[0] != 'mjpeg', abs(m[1]*m[2]-720*480), abs(m[3]-25)))
    # Exercise each advertised format before trying lower-ranked sizes/rates.
    first, rest, seen = [], [], set()
    for mode in result:
        if mode[0] in seen:
            rest.append(mode)
        else:
            first.append(mode)
            seen.add(mode[0])
    return (first + rest)[:12] + [None]  # Final fallback: driver's default format.


def controls(device, requested):
    supported = query(device, '--list-ctrls')
    applied, skipped = {}, []
    for setting in requested.split(','):
        if not setting or '=' not in setting:
            continue
        key, value = setting.split('=', 1)
        match = re.search(r'^\s*' + re.escape(key) + r'\s+.*?min=(-?\d+) max=(-?\d+).*?step=(\d+)', supported, re.M)
        if not match:
            skipped.append(key)
            continue
        low, high, step = map(int, match.groups())
        value = max(low, min(high, int(value)))
        value = low + ((value-low)//max(1, step))*max(1, step)
        try:
            query(device, '--set-ctrl=' + key + '=' + str(value))
            applied[key] = value
        except (OSError, RuntimeError, subprocess.SubprocessError) as error:
            log(f'Control {key} unavailable: {error}')
            skipped.append(key)
    return dict(applied=applied, skipped=skipped)


def save(name, data):
    tmp = RUNTIME / (name + '.tmp')
    tmp.write_text(data)
    os.replace(tmp, RUNTIME / name)


def ffmpeg_command(device, mode, filters):
    args = ['ffmpeg', '-hide_banner', '-loglevel', 'warning', '-nostdin',
            '-stats_period', '1', '-progress', 'pipe:1', '-thread_queue_size', '4', '-f', 'v4l2']
    if mode:
        fmt, width, height, fps = mode
        args += ['-input_format', fmt, '-video_size', f'{width}x{height}', '-framerate', str(fps)]
    args += ['-i', device, '-an', '-vf', filters + ',scale=trunc(iw/2)*2:trunc(ih/2)*2',
             '-c:v', 'libx264', '-preset', 'veryfast', '-tune', 'zerolatency',
             '-profile:v', 'baseline', '-b:v', '2500k', '-maxrate', '3000k',
             '-bufsize', '750k', '-g', '25', '-keyint_min', '25', '-sc_threshold', '0',
             '-bf', '0', '-threads', '2', '-f', 'rtsp', '-rtsp_transport', 'tcp',
             'rtsp://127.0.0.1:554/live']
    return args


def stream(args, stall_timeout=15):
    """Return only on failure; advancing frame count is the watchdog heartbeat."""
    process = subprocess.Popen(args, stdout=subprocess.PIPE, bufsize=0)
    last_frame, last_change = 0, time.monotonic()
    pending, report = b'', []
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    try:
        while process.poll() is None:
            for key, _ in selector.select(timeout=1):
                chunk = os.read(key.fileobj.fileno(), 8192)
                if not chunk:
                    continue
                pending += chunk
                while b'\n' in pending:
                    line, pending = pending.split(b'\n', 1)
                    line = line.decode(errors='replace') + '\n'
                    report.append(line)
                    if line.startswith('progress='):
                        frame = next((int(x.split('=')[1]) for x in report if x.startswith('frame=')), 0)
                        if frame > last_frame:
                            last_frame, last_change = frame, time.monotonic()
                            save('progress', ''.join(report))
                        report = []
            if time.monotonic() - last_change > stall_timeout:
                log(f'No advancing frames for {stall_timeout} seconds; trying next capture mode')
                break
    finally:
        selector.close()
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        process.stdout.close()
    log(f'Capture attempt ended: exit={process.returncode}')


def main():
    RUNTIME.mkdir(exist_ok=True)
    state = {'device': '', 'mode': None, 'attempt': 0, 'error': '', 'controls': {}}
    for device in devices(os.environ.get('CVBS_DEVICE', 'auto')):
        try:
            candidates = modes(query(device, '--list-formats-ext'))
        except (RuntimeError, subprocess.SubprocessError) as error:
            log(f'Cannot enumerate modes: {error}; trying driver defaults')
            candidates = [None]
        try:
            settings = controls(device, os.environ.get('CVBS_CAPTURE_CONTROLS', ''))
        except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
            settings = {'applied': {}, 'skipped': [], 'error': str(error)}
            log(f'Hardware controls unavailable; continuing: {error}')
        for mode in candidates:
            if not Path(device).exists():
                break
            state.update(device=device, mode=mode, controls=settings, attempt=state['attempt']+1)
            save('selection.json', json.dumps(state))
            save('progress', '')
            log(f'Trying device={device} mode={mode or "driver default"} controls={settings}')
            stream(ffmpeg_command(device, mode, os.environ.get('CVBS_VIDEO_FILTER', 'format=yuv420p')))
    state['error'] = 'No working USB V4L2 capture mode; automatic rescan in 3 seconds'
    save('selection.json', json.dumps(state))
    log(state['error'])
    raise SystemExit(1)


def patch_hid():
    """Allow the RAM patch only on the same physical USB device as the stream."""
    selected = json.loads((RUNTIME / 'selection.json').read_text())['device']
    parent = usb_parent(selected)
    if (parent / 'idVendor').read_text().strip() != '534d' or (parent / 'idProduct').read_text().strip() != '0021':
        raise RuntimeError('Selected capture does not support the MacroSilicon RAM patch')
    for entry in Path('/sys/class/hidraw').glob('hidraw*'):
        if parent in (entry / 'device').resolve().parents:
            return str(Path('/dev') / entry.name)
    raise RuntimeError('Selected capture has no HID interface')


if __name__ == '__main__':
    if '--patch-hid' in sys.argv:
        try:
            print(patch_hid())
        except (OSError, RuntimeError, KeyError, ValueError) as error:
            print(str(error), file=sys.stderr)
            raise SystemExit(78)
    else:
        main()
