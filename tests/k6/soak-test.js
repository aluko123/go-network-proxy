import http from 'k6/http';
import { check, sleep } from 'k6';

// Soak test: sustained load to find memory leaks, degradation
export let options = {
    stages: [
        { duration: '2m', target: 100 },   // Ramp up
        { duration: '1h', target: 100 },   // Hold for 1 hour
        { duration: '2m', target: 0 },     // Ramp down
    ],
};

export default function () {
    const res = http.get('http://localhost:9000', {
        proxy: 'http://localhost:8080'
    });
    
    check(res, {
        'status 200 or 429': (r) => r.status === 200 || r.status === 429,
        'response time stable': (r) => r.timings.duration < 1000, // No degradation
    });
    
    sleep(1);
}
