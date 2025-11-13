import http from 'k6/http';
import { check } from 'k6';

export let options = {
	stages: [
	    { duration: '30s', target: 50 },
	    { duration: '1m', target: 100 },
	    { duration: '30s', target: 0 },
   	],
};


export default function () {
   const res = http.get('http://localhost:9000', {proxy: 'http://localhost:8080'});
   
   // Log actual status to see what's happening
   if (res.status !== 200) {
       console.log(`Got status: ${res.status}, error: ${res.error}`);
   }
   
   check (res, {
	'status is 200': (r) => r.status === 200,
        'response time < 500ms': (r) => r.timings.duration < 500,
   });
}

