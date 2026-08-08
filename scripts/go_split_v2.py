#!/usr/bin/env python3
"""Split a Go file at function boundaries, preserving correct package+imports."""
import sys, os

def extract_import_block(lines):
    """Extract the import block from Go source lines."""
    result = []
    in_import = False
    for line in lines:
        s = line.strip()
        if s.startswith('import ('):
            in_import = True
            result.append(line)
        elif in_import:
            result.append(line)
            if s == ')':
                break
        elif s.startswith('import ') and not s.startswith('import ('):
            result.append(line)
            break
    return ''.join(result)

def extract_pkg_name(lines):
    for line in lines:
        if line.startswith('package '):
            return line.strip().split()[1]
    return 'main'

def main():
    if len(sys.argv) < 5 or (len(sys.argv) - 2) % 3 != 0:
        print(f"Usage: {sys.argv[0]} INPUT_FILE OUT_FILE START_LINE END_LINE [...]")
        sys.exit(1)
    
    filepath = sys.argv[1]
    with open(filepath) as f:
        lines = f.readlines()
    
    pkg_name = extract_pkg_name(lines)
    import_block = extract_import_block(lines)
    
    i = 2
    while i < len(sys.argv):
        out_file = sys.argv[i]
        start = int(sys.argv[i+1])  # 1-indexed, inclusive
        end = int(sys.argv[i+2])     # 1-indexed, inclusive
        i += 3
        
        body = ''.join(lines[start-1:end])
        content = f'package {pkg_name}\n\n{import_block}\n\n{body}'
        
        os.makedirs(os.path.dirname(out_file) or '.', exist_ok=True)
        with open(out_file, 'w') as f:
            f.write(content)
        print(f'  {os.path.basename(out_file)}: {end - start + 1} lines (body)')

if __name__ == '__main__':
    main()
