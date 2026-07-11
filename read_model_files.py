import os
base = '/home/nergis/Dev/OmniRelay/backend/internal/proxy'
files = os.listdir(base)
targets = [f for f in files if f.endswith('.go') and f.startswith('the ')]
print(f"Found {len(targets)} target files")
for t in targets:
    fpath = os.path.join(base, t)
    print(f'=== {t} ===')
    with open(fpath) as f:
        print(f.read())
    print()
