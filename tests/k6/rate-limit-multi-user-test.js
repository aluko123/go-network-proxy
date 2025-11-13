import http from 'k6/http';
import { check } from 'k6';

export let options = {
    scenarios: {
        // Each VU acts as a different "user" (though same IP in k6)
        constant_load: {
            executor: 'constant-vus',
            vus: 5,  // 5 concurrent users
            duration: '10s',
        },
    },
};

export default function () {
    const res = http.get('http://localhost:9000', {
        proxy: 'http://localhost:8080'
    });

    check(res, {
        'success (200)': (r) => r.status === 200,
        'rate limited (429)': (r) => r.status === 429,
    });
}
