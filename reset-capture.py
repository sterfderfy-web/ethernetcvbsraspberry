#!/usr/bin/env python3
"""Reset only the configured MacroSilicon USB capture, after patch rollback."""
import fcntl
from pathlib import Path
import subprocess
import time


def run(*args):
    return subprocess.run(args, check=True, timeout=45, capture_output=True, text=True)


def usb_device(video):
    node = Path(video).resolve(strict=True)
    if not node.name.startswith('video'):
        raise RuntimeError('Configured device is not a video node')
    for parent in (Path('/sys/class/video4linux') / node.name / 'device').resolve().parents:
        if (parent / 'idVendor').exists():
            if (parent / 'idVendor').read_text().strip() != '534d' or (parent / 'idProduct').read_text().strip() != '0021':
                raise RuntimeError('Configured capture is not the supported USB device')
            return Path('/dev/bus/usb') / f'{int((parent / "busnum").read_text()):03d}' / f'{int((parent / "devnum").read_text()):03d}'
    raise RuntimeError('USB parent not found')


def main():
    config = Path('/etc/cvbs-camera/device.env').read_text()
    video = config.split('CVBS_DEVICE=', 1)[1].splitlines()[0]
    usb = usb_device(video)  # Resolve and validate before stopping anything.
    active = subprocess.run(['systemctl', 'is-active', '--quiet', 'cvbs-weak-video']).returncode == 0
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
        run('systemctl', 'start', 'cvbs-weak-video')
    print('USB reset completed; inspect frame and patch logs to verify video', flush=True)


if __name__ == '__main__':
    main()
