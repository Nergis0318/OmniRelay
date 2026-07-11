import os, sys

# Find the actual directory name with model
root = '/home/nergis/Dev/OmniRelay'
for entry in os.scandir(root):
    if entry.is_dir() and 'model' in entry.name:
        print(f"dir name: {repr(entry.name)}")
        print(f"dir path: {repr(entry.path)}")
        for root2, dirs, files in os.walk(entry.path):
            for f in files:
                if f.endswith('.go'):
                    fp = os.path.join(root2, f)
                    print(fp)
