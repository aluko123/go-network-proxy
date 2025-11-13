import http from 'k6/http';

export let options = {
    vus: 1,
    iterations: 5,
};

export default function () {
    // Try to use proxy via environment variable instead
    const res = http.get('http://localhost:9000');
    console.log(`Status: ${res.status}`);
}
