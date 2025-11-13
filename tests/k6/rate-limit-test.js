import http from 'k6/http';
import { check, sleep } from 'k6';

export let options = {
    vus: 1,  // Single user to test per-IP limiting
    iterations: 20,  // Send 20 requests total
};

export default function () {
    const res = http.get('http://localhost:9000', {
        proxy: 'http://localhost:8080'
    });

    const isRateLimited = res.status === 429;
    const isSuccess = res.status === 200;

    check(res, {
        'success (200)': (r) => r.status === 200,
        'rate limited (429)': (r) => r.status === 429,
    });

    if (isRateLimited) {
        console.log(`Request ${__ITER + 1}: RATE LIMITED - ${res.body}`);
    } else if (isSuccess) {
        console.log(`Request ${__ITER + 1}: SUCCESS`);
    } else {
        console.log(`Request ${__ITER + 1}: ERROR ${res.status}`);
    }

    // Don't sleep - send requests as fast as possible
}
