#!/usr/bin/env python3
"""Simple test script to verify Python runtime execution."""

import sys
import os

print("Python Runtime Test")
print(f"Python version: {sys.version}")
print(f"Working directory: {os.getcwd()}")

# Check for environment variables
api_key = os.environ.get("TEST_API_KEY", "not_set")
print(f"TEST_API_KEY: {api_key}")

print("Test completed successfully!")
