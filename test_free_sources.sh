#!/usr/bin/env bash
# Test free image sources: Pollinations (gen), Openverse (photo search). GPU check too.
set -u

echo "--- GPU ---"
nvidia-smi --query-gpu=name,memory.total --format=csv,noheader 2>/dev/null || echo "no NVIDIA GPU"

echo "--- POLLINATIONS ---"
PROMPT=$(python3 -c "import urllib.parse; print(urllib.parse.quote('flat illustration, wine bottle and glass on pastel green background, minimal, modern'))")
code=$(curl -sL -o /home/kieran/edele-idea-lab/img/test_pollinations.jpg -w '%{http_code}' --max-time 90 \
  "https://image.pollinations.ai/prompt/${PROMPT}?width=768&height=768&nologo=true&model=flux")
echo "pollinations HTTP $code"
file /home/kieran/edele-idea-lab/img/test_pollinations.jpg 2>/dev/null

echo "--- OPENVERSE ---"
curl -s --max-time 30 -o /tmp/openverse.json \
  "https://api.openverse.org/v1/images/?q=wine%20cellar&page_size=3"
python3 - <<'EOF'
import json
try:
    d = json.load(open('/tmp/openverse.json'))
except Exception as e:
    print('openverse unparseable:', e); raise SystemExit
n = d.get('result_count', 0)
print('openverse result_count:', n)
for r in d.get('results', [])[:3]:
    print(' -', r.get('title','')[:40], '|', r.get('url','')[:90])
EOF