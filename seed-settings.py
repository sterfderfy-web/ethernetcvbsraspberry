import json
from pathlib import Path
import re
import panel

root = panel.ROOT
settings = panel.DEFAULT.copy()
if (root / 'settings.json').exists():
    panel.read_settings()
elif (root / 'image.env').exists():
    # Migrate the previous installed profile without resetting its correction.
    text = (root / 'image.env').read_text()
    controls, filters = text.split('CVBS_CAPTURE_CONTROLS=', 1)[1].split('\n', 1)[0], text.split('CVBS_VIDEO_FILTER=', 1)[1].split('\n', 1)[0]
    for key in ('brightness', 'contrast', 'saturation'):
        settings[key] = int(re.search(r'(?:^|,)' + key + r'=([0-9]+)', controls).group(1))
    eq = filters.split('eq=', 1)[1].split(',', 1)[0]
    for key, alias in dict(lift='brightness', gain='contrast', color='saturation', gamma='gamma', gamma_weight='gamma_weight').items():
        settings[key] = float(re.search(r'(?:^|:)' + alias + r'=([-0-9.]+)(?:$|:)', eq).group(1))
    settings['denoise'] = float(re.search(r'hqdn3d=([0-9.]+)', filters).group(1))
    settings['sharpness'] = float(re.search(r'unsharp=5:5:([0-9.]+)', filters).group(1))
    panel.atomic(root / 'settings.json', json.dumps(panel.validate(settings)))
else:
    panel.atomic(root / 'image.env', panel.environment(settings))
    panel.atomic(root / 'settings.json', json.dumps(settings))
