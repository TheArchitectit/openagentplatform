#!/usr/bin/env python3
"""go_splitter.py — Split a Go file into domain-specific files.

Usage:
  python3 go_splitter.py <source.go> <splits.toml>

Splits format (inline TOML-like):
  filename = start_line-end_line

The script:
1. Reads the original file's package + import block
2. Extracts each section by line range
3. Computes which imports are used (handling /v5, /v2 version suffixes)
4. Writes each split file with only the needed imports
5. Handles shared consts/vars by keeping them in the FIRST split file only

Key fix: Go packages like github.com/jackc/pgx/v5 have local name "pgx"
(not "v5"). This script correctly resolves local names by stripping /vN suffixes.
"""

import re
import sys
from pathlib import Path


def go_local_name(import_path: str) -> str:
    """Compute the Go local name for an import path.

    Rules:
    - Strip /vN version suffixes (e.g., pgx/v5 → pgx)
    - Use the last path component
    - Handle dot-separated names (e.g., json-iterator → json)
    """
    # Strip version suffixes like /v5, /v2
    path = re.sub(r'/v\d+(?:/|$)', '/', import_path)
    # Get last component
    name = path.rstrip('/').split('/')[-1]
    # Replace dashes/dots with underscores (Go convention)
    name = re.sub(r'[-.]+', '_', name)
    return name


def parse_imports(import_block: str) -> list[tuple[str, str, str]]:
    """Parse Go import block into [(alias, path, original_line), ...]."""
    imports = []
    for line in import_block.strip().split('\n'):
        line = line.strip()
        if not line or line == '(' or line == ')':
            continue
        alias_m = re.match(r'^(\w+)\s+"(.+)"$', line)
        if alias_m:
            alias, pkg = alias_m.group(1), alias_m.group(2)
            imports.append((alias, pkg, '\t' + line))
        else:
            pkg_m = re.match(r'^"(.+)"$', line)
            if pkg_m:
                pkg = pkg_m.group(1)
                imports.append((go_local_name(pkg), pkg, '\t' + line))
    return imports


def needed_imports(code: str, all_imports: list[tuple[str, str, str]]) -> list[str]:
    """Return only the imports whose local names appear in the code."""
    needed = []
    for alias, pkg, line in all_imports:
        # Use word boundary search for the local name
        if re.search(r'\b' + re.escape(alias) + r'\b', code):
            needed.append(line)
    return needed


def find_package_and_imports(lines: list[str]) -> tuple[str, int]:
    """Find the package declaration and end of import block.

    Returns (package_line, import_end_line_1indexed).
    """
    pkg_line = None
    in_import = False
    import_end = 0
    for i, line in enumerate(lines):
        if line.startswith('package '):
            pkg_line = line.rstrip()
        if line.strip() == 'import (':
            in_import = True
        elif in_import and line.strip() == ')':
            import_end = i + 1  # 1-indexed
            in_import = False
            break
    return pkg_line or 'package main', import_end


def find_shared_declarations(lines: list[str], import_end: int) -> tuple[list[str], int]:
    """Find shared const/var blocks between imports and first function.

    Returns (shared_lines, shared_end_1indexed).
    """
    shared = []
    shared_end = import_end
    for i in range(import_end, len(lines)):
        line = lines[i]
        if line.startswith('func ') or line.startswith('type '):
            shared_end = i  # 0-indexed, exclusive
            break
        shared.append(line)
    else:
        shared_end = len(lines)
    return shared, shared_end


def main():
    if len(sys.argv) < 3:
        print(f"Usage: {sys.argv[0]} <source.go> <splits_config>")
        print(f"  splits_config: filename=start-end filename=start-end ...")
        print(f"  Example: store_alerts.go=21-507 store_state.go=508-687")
        sys.exit(1)

    source_path = Path(sys.argv[1])
    splits_args = sys.argv[2:]

    with open(source_path) as f:
        lines = f.readlines()

    # Parse package + imports
    pkg_line, import_end = find_package_and_imports(lines)
    import_text = ''.join(lines[:import_end])
    all_imports = parse_imports(import_text)

    # Find shared const/var blocks (between imports and first func/type)
    shared_decls, shared_end = find_shared_declarations(lines, import_end)
    shared_text = ''.join(shared_decls).strip()

    # Parse split specs
    splits = []
    for spec in splits_args:
        fname, range_str = spec.split('=', 1)
        start_str, end_str = range_str.split('-', 1)
        start, end = int(start_str), int(end_str)
        splits.append((fname, start, end))

    # Generate each split file
    for idx, (fname, start, end) in enumerate(splits):
        # Extract body code
        body = ''.join(lines[start - 1:end])

        # First file gets shared declarations; others don't
        if idx == 0 and shared_text:
            section_code = shared_text + '\n\n' + body
        else:
            section_code = body

        # Compute needed imports
        imports = needed_imports(section_code, all_imports)

        # Build the file
        content = pkg_line + '\n\n'
        if imports:
            content += 'import (\n'
            for imp_line in imports:
                content += imp_line + '\n'
            content += ')\n\n'
        content += section_code

        # Clean up multiple blank lines
        content = re.sub(r'\n{3,}', '\n\n', content)

        out_path = source_path.parent / fname
        with open(out_path, 'w') as f:
            f.write(content)

        line_count = len(content.splitlines())
        print(f"  {fname}: {line_count} lines ({len(imports)} imports)")


if __name__ == '__main__':
    main()
