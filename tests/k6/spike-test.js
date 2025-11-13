import http from 'k6/http';
import { check } from 'k6';

// Spike test: sudden burst of traffic (news event, marketing)
export let options = {
    stages: [
        { duration: '10s', target: 50 },    // Normal load
        { duration: '10s', target: 500 },   // SPIKE!
        { duration: '30s', target: 500 },   // Hold spike
        { duration: '10s', target: 50 },    // Return to normal
        { duration: '1m', target: 50 },     // Recovery
    ],
};

export default function () {
    const res = http.get('http://localhost:9000', {
        proxy: 'http://localhost:8080'
    });
    
    check(res, {
        'survived spike': (r) => r.status !== 0 && r.status !== 502,
    });
}
