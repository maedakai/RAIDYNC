import http from 'k6/http';
import {check, sleep } from 'k6';
import exec from 'k6/execution';
import{Rate} from 'k6/metrics';

const VUS =  Number(__ENV.VUS || 4);
// テストユーザー共通のパスワード。プールのポリシー（8 文字以上・大文字・小文字・数字・記号）を満たす値にする。
// scripts/create-load-test-users.sh が設定した値で、InitiateAuth のログインに使う。変えるときは両方を揃える。
const PASSWORD = 'LoadTest#2026';
// テストユーザーのメールアドレスの先頭に付ける固定の文字列（k6run+<連番>@example.com）。
// ユーザーは scripts/create-load-test-users.sh で一度だけ作って使い回す（毎回作ると Cognito の MAU が増えるため）。
// 作成スクリプトと掃除スクリプト（cleanup-load-test-users.sh）も同じ値を使うので、変えるときは揃える。
const PREFIX = 'k6run+';
const API_URL = __ENV.API_URL;
const ITERATIONS = Number(__ENV.ITERATIONS || 20)

export const options = {
    scenarios: {
        loop: {
            executor: 'per-vu-iterations',
            vus: VUS,
            iterations: ITERATIONS,
            maxDuration: '10m',
            gracefulStop: '80s',
        }
    },
    thresholds: {
        http_req_failed: ['rate==0'],
        matched: ['rate==1'],   // 4 で作る自前の指標の名前
    },
    setupTimeout: '20m',  // setup() の制限時間を 20 分に設定
};

export function setup() {
    if(!API_URL){
        throw new Error('API_URL environment variable is not set');
    }
    if(VUS%4!==0){
        throw new Error(`VUS is not a Multiples of 4 (got ${VUS})`)
    }
    const tokens = [];
    // questId を実行ごとに変え、前回の実行で残った待ちチケットとマッチしないようにする
    const runId = Date.now();
    try{
        var i = 0;
        for (i = 0; i < VUS; i++) {
            const username = `${PREFIX}${i}@example.com`;
            const body = JSON.stringify({
                AuthFlow: 'USER_PASSWORD_AUTH',
                ClientId: '2i3d508aqfla13dmksip64s5sv',
                AuthParameters: {
                    USERNAME: username,
                    PASSWORD: PASSWORD
                }
            })
            const response = http.post(
                'https://cognito-idp.ap-northeast-1.amazonaws.com/',
                body,
                {headers:{
                    'Content-Type': 'application/x-amz-json-1.1',
                    'X-Amz-Target': 'AWSCognitoIdentityProviderService.InitiateAuth'
                }})
            if (response.status !== 200) {
                // ユーザーが VUS 人より少ないと、ここで失敗する（UserNotFoundException など）
                throw new Error(`Failed to login ${username}: ${response.status} ${response.body}`
                    + ` (VUS 人以上いるか確認: ./scripts/create-load-test-users.sh ${VUS})`);
            }
            tokens.push(response.json().AuthenticationResult.IdToken);
        }
    }
    catch (err) {
        console.error(`Error during setup: ${err.message}`);
        throw err;  // エラーを再スローしてテストを失敗させる
    }

    return {questId: `load-test-${runId}`, tokens: tokens};
}

const matched = new Rate('matched');
const matchFailed = new Rate('matchFailed')
export default function(data) {
    const deadline = Date.now() + 70 * 1000;
    var response = http.post(`${API_URL}/match/request`, 
        JSON.stringify({'questId': data.questId}),
        { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${data.tokens[exec.vu.idInTest - 1]}` } });
        const ticketId = response.json().ticketId;
    check(response, { 'request is 200': (r) => r.status === 200 })
    let state = '';
    while (state !== 'MATCHED' && state !== 'TIMEOUT'&&state!== 'FAILED'&& Date.now() < deadline) {
        sleep(2);
        response = http.get(`${API_URL}/match/status/${ticketId}`,
        {headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${data.tokens[exec.vu.idInTest - 1]}` } });
        state = response.json().status;
    }
    check(state, {
        'match status is MATCHED': (s) => s === 'MATCHED',
    });
    matched.add(state === 'MATCHED');
    matchFailed.add(state === 'FAILED');
}