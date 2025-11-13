import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const rateLimited = new Counter('rate_limited');
const allowed = new Counter('allowed');

// Aggressive test: SHOULD hit rate limits
export let options = {
    vus: 1,              // Single user
    duration: '10s',     // 10 seconds
    iterations: 1000,    // Try 1000 requests
};

export default function () {
    const res = http.get('http://localhost:9000', {
        proxy: 'http://localhost:8080'
    });
    
    if (res.status === 429) {
        rateLimited.add(1);
    } else if (res.status === 200) {
        allowed.add(1);
    }
    
    check(res, {
        'got a response': (r) => r.status === 200 || r.status === 429,
    });
}
