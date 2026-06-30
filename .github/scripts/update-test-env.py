#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
# SPDX-License-Identifier: Apache-2.0
#
# Rewrites key=value lines in a .env file with values from environment variables.
# Usage: python3 update-test-env.py <path-to-test.env>
#
# Uses regex-based replacement instead of sed so that secret values containing
# shell metacharacters (& / etc.) are handled safely.

import os
import re
import sys

path = sys.argv[1]
replacements = {
    "GITHUB_TOKEN": os.environ["TEST_GITHUB_TOKEN"],
    "GITHUB_CLIENT_ID": os.environ["TEST_GITHUB_CLIENT_ID"],
    "GITHUB_CLIENT_SECRET": os.environ["TEST_GITHUB_CLIENT_SECRET"],
}
# Optional multiline values written as heredoc blocks (e.g. PEM keys).
# The existing placeholder line (KEY=...) is replaced with a heredoc so
# gen-values.sh read_var_multiline can parse it correctly.
def normalize_multiline(val):
    """GitHub Actions injects multiline secrets with literal \\n sequences.
    Normalize them to real newlines so PEM keys remain valid."""
    if "\\n" in val:
        val = val.replace("\\n", "\n")
    return val.strip()

multiline_replacements = {}
private_key = os.environ.get("TEST_GITHUB_PRIVATE_KEY", "")
if private_key:
    multiline_replacements["GITHUB_PRIVATE_KEY"] = normalize_multiline(private_key)

lines = open(path).readlines()
with open(path, "w") as f:
    for line in lines:
        matched = False
        for key, val in replacements.items():
            if re.match(rf"^{key}=", line):
                f.write(f"{key}={val}\n")
                matched = True
                break
        if not matched:
            for key, val in multiline_replacements.items():
                if re.match(rf"^{key}=", line):
                    f.write(f"{key}<<HEREDOC\n{val}\nHEREDOC\n")
                    matched = True
                    break
        if not matched:
            f.write(line)
