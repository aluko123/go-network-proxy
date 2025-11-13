import http from 'k6/http';
import { check } from 'k6';

// Stress test: find the breaking point
export let options = {
    stages: [
        { duration: '2m', target: 100 },   // Ramp to 100
        { duration: '2m', target: 200 },   // Increase to 200
        { duration: '2m', target: 300 },   // Push to 300
        { duration: '2m', target: 400 },   // Stress at 400
        { duration: '2m', target: 500 },   // Breaking point?
        { duration: '5m', target: 500 },   // Hold at max
        { duration: '2m', target: 0 },     // Recovery
    ],
};

export default function () {
    const res = http.get('http://localhost:9000', {
        proxy: 'http://localhost:8080'
    });
    
    check(res, {
        'not server error': (r) => r.status < 500,
    });
}
