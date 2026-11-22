import http from 'k6/http';
import {check, sleep } from 'k6';
import exec from 'k6/execution';
import{Rate} from 'k6/metrics';

const VUS = 40;
export const options = {
    scenarios: {
        loop: {
            executor: 'constant-vus',
            vus: VUS,
            duration: '3m',
            gracefulStop: '80s',
        }
    },
    thresholds: {
    http_req_failed: ['rate==0'],
    matched: ['rate==1'],   // 4 で作る自前の指標の名前
    },
};

export function setup() {
    const runId = Date.now();   // 例: 1791024658551
    const questId = `loadtest-${runId}`;
    // questId: `loadtest-${runId}`、メール: `test${i}${runId}@example.com`
    // テストユーザー作成する
    const tokens = [];
    for (let i = 0; i < VUS; i++) {
        var response= http.post('http://localhost:9229/signup',
                JSON.stringify({'email': `test${i}${runId}@example.com`, 'password': 'password'}));
        if (response.status !== 201)
            throw new Error(`Failed to create test user: ${response.status} ${response.body}`);
            
        response= http.post('http://localhost:9229/login',
                JSON.stringify({'email': `test${i}${runId}@example.com`, 'password': 'password'}));
        if (response.status !== 200)
            throw new Error(`Failed to login test user: ${response.status} ${response.body}`);
        tokens.push(response.json().id_token);
    }
    console.log(`questId: ${questId}`);
    return {questId: questId, tokens: tokens};
}

const matched = new Rate('matched');  // 4. 指標の作成
export default function(data) {
    const deadline = Date.now() + 70 * 1000; // 70秒後にタイムアウト
    // match/request
    var response = http.post('http://localhost:8080/match/request', 
        JSON.stringify({'questId': data.questId}),
        { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${data.tokens[exec.vu.idInTest - 1]}` } });
    const ticketId = response.json().ticketId;
    check(response, { 'request is 200': (r) => r.status === 200 })
        // match/status
    let state = '';
    while (state !== 'MATCHED' && state !== 'TIMEOUT'&& Date.now() < deadline) {
        sleep(2);
        response = http.get(`http://localhost:8080/match/status/${ticketId}`,
        {headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${data.tokens[exec.vu.idInTest - 1]}` } });
        state = response.json().status;
    }
    check(state, {
        'match status is MATCHED': (s) => s === 'MATCHED',
    });
    matched.add(state === 'MATCHED');
}