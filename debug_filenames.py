import os
base = '/home/nergis/Dev/OmniRelay/backend/internal/proxy'
files = os.listdir(base)
for f in sorted(files):
    if f.endswith('.go'):
        print(f"{repr(f):50s} hex={f.encode('utf-8').hex()}")
