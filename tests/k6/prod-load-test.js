import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';

// Custom metrics
const rateLimitHits = new Counter('rate_limit_hits');
const successfulRequests = new Counter('successful_requests');

// Production-like load profile
export let options = {
    stages: [
        // Warm up
        { duration: '1m', target: 20 },   // Ramp to 20 users
        
        // Normal business hours
        { duration: '3m', target: 50 },   // Steady 50 users
        
        // Peak hours (lunch, morning)
        { duration: '2m', target: 100 },  // Peak: 100 users
        { duration: '2m', target: 100 },  // Hold peak
        
        // Spike (viral content, marketing campaign)
        { duration: '30s', target: 200 }, // Sudden spike
        { duration: '1m', target: 200 },  // Hold spike
        
        // Back to normal
        { duration: '1m', target: 50 },   // Cool down
        
        // Ramp down
        { duration: '1m', target: 0 },    // Graceful shutdown
    ],
    
    thresholds: {
        'http_req_duration': ['p(95)<500'], // 95% under 500ms
        'rate_limit_hits': ['count<100'],   // Less than 100 rate limits
        'http_req_failed': ['rate<0.05'],   // Less than 5% errors
    },
};

// Simulate different user behaviors
export default function () {
    // 70% normal users, 30% heavy users
    const isHeavyUser = Math.random() < 0.3;
    
    if (isHeavyUser) {
        // Heavy users: rapid requests (may hit rate limit)
        for (let i = 0; i < 3; i++) {
            makeRequest();
            sleep(0.1); // 100ms between requests
        }
    } else {
        // Normal users: single request with realistic delay
        makeRequest();
        sleep(Math.random() * 3 + 1); // 1-4 seconds think time
    }
}

function makeRequest() {
    const res = http.get('http://localhost:9000', {
        proxy: 'http://localhost:8080',
        tags: { name: 'proxy_request' },
    });
    
    const passed = check(res, {
        'status is 200': (r) => r.status === 200,
        'response time OK': (r) => r.timings.duration < 500,
    });
    
    if (res.status === 429) {
        rateLimitHits.add(1);
    } else if (res.status === 200) {
        successfulRequests.add(1);
    }
}
