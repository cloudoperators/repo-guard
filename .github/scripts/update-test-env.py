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
lines = open(path).readlines()
with open(path, "w") as f:
    for line in lines:
        for key, val in replacements.items():
            if re.match(rf"^{key}=", line):
                line = f"{key}={val}\n"
                break
        f.write(line)
