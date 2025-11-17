#!/usr/bin/env python3
"""
Simple Python script example for deps run
"""
import sys
import platform

def main():
    print(f"Hello from Python {platform.python_version()}!")
    print(f"Platform: {platform.system()} {platform.machine()}")

    if len(sys.argv) > 1:
        print(f"Arguments: {' '.join(sys.argv[1:])}")

    return 0

if __name__ == "__main__":
    sys.exit(main())
