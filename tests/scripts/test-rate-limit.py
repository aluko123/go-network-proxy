#!/usr/bin/env python3
import requests
from collections import Counter

# Configure proxy
proxies = {
    'http': 'http://localhost:8080',
}

# Send 200 requests
print("Sending 200 requests through proxy...")
results = Counter()

for i in range(1, 201):
    try:
        r = requests.get('http://localhost:9000', proxies=proxies, timeout=2)
        results[r.status_code] += 1
        if i % 20 == 0:
            print(f"Progress: {i}/200")
    except Exception as e:
        results['error'] += 1

print("\n=== Results ===")
for status, count in sorted(results.items()):
    if isinstance(status, int):
        print(f"HTTP {status}: {count}")
    else:
        print(f"{status}: {count}")

total_requests = sum(v for k, v in results.items() if isinstance(k, int))
print(f"\nTotal requests: {total_requests}")
print(f"✅ HTTP 200 (allowed): {results[200]}")
print(f"❌ HTTP 429 (rate limited): {results[429]}")
if results[429] > 0:
    print(f"\nRate limit effectiveness: {results[429] / total_requests * 100:.1f}%")
