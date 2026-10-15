#!/usr/bin/env python3
"""Reset the selected USB V4L2 capture, after any active patch rollback."""
import fcntl
from pathlib import Path
import subprocess
import time
import json
import capture_auto


def run(*args):
    return subprocess.run(args, check=True, timeout=45, capture_output=True, text=True)


def usb_device(video):
    parent = capture_auto.usb_parent(video)
    return Path('/dev/bus/usb') / f'{int((parent / "busnum").read_text()):03d}' / f'{int((parent / "devnum").read_text()):03d}'


def selected_device():
    try:
        device = json.loads((capture_auto.RUNTIME / 'selection.json').read_text())['device']
        if device and Path(device).exists():
            return device
    except (OSError, ValueError, KeyError):
        pass
    config = Path('/etc/cvbs-camera/device.env').read_text()
    preferred = config.split('CVBS_DEVICE=', 1)[1].splitlines()[0]
    candidates = capture_auto.devices(preferred)
    if not candidates:
        raise RuntimeError('No USB video capture connected')
    return candidates[0]


def main():
    video = selected_device()
    usb = usb_device(video)  # Resolve and validate before stopping anything.
    active = subprocess.run(['systemctl', 'is-active', '--quiet', 'cvbs-weak-video']).returncode == 0
    enabled = subprocess.run(['systemctl', 'is-enabled', '--quiet', 'cvbs-weak-video']).returncode == 0
    run('systemctl', 'stop', 'cvbs-weak-video')
    if active:
        folder = Path('/var/lib/cvbs-patch/current-run').read_text().strip()
        if 'restoration failures: []' not in (Path(folder) / 'restore-status.txt').read_text():
            raise RuntimeError('Patch rollback not verified; USB reset aborted')
    try:
        run('systemctl', 'stop', 'cvbs-capture')
        with usb.open('wb', buffering=0) as device:
            fcntl.ioctl(device, 0x5514, 0)  # Linux USBDEVFS_RESET
        time.sleep(2)
        run('udevadm', 'settle', '--timeout=10')
    finally:
        run('systemctl', 'start', 'cvbs-capture')
        if active or enabled:
            run('systemctl', 'start', 'cvbs-weak-video')
    print('USB reset completed; inspect frame and patch logs to verify video', flush=True)


if __name__ == '__main__':
    main()
