"""Keep one ffmpeg progress report, atomically and without growing logs."""
from pathlib import Path
import os
import sys

target = Path('/run/cvbs-capture/progress')
report = []
last_frame = None
for line in sys.stdin:
    report.append(line)
    if line.startswith('progress='):
        frame = next((item.strip() for item in report if item.startswith('frame=')), None)
        if frame != last_frame:
            temporary = target.with_suffix('.tmp')
            temporary.write_text(''.join(report))
            os.replace(temporary, target)
            last_frame = frame
        report = []
