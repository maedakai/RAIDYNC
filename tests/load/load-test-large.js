import http from 'k6/http';
import {check, sleep } from 'k6';
import exec from 'k6/execution';
import{Rate} from 'k6/metrics';
import{Endpoint,SignatureV4} from 'https://jslib.k6.io/aws/0.14.0/signature.js';

const VUS =  Number(__ENV.VUS || 4);
// テストユーザー共通のパスワード。プールのポリシー（8 文字以上・大文字・小文字・数字・記号）を満たす値にする。
// AdminSetUserPassword で設定し、InitiateAuth のログインでも同じ値を使う。
const PASSWORD = 'LoadTest#2026';
// テストユーザーのメールアドレスの先頭に付ける固定の文字列（k6run+<runId>+<連番>@example.com）。
// scripts/cleanup-load-test-users.sh がこの値の前方一致で削除対象を選ぶので、変えるときは両方を揃える。
const PREFIX = 'k6run+';
const API_URL = __ENV.API_URL;

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
    setupTimeout: '20m',  // setup() の制限時間を 20 分に設定
};

export function setup() {
    if(!API_URL){
        throw new Error('API_URL environment variable is not set');
    }
    const tokens = [];
    // 署名の作成
    const signer = createSigner();
    const runId = Date.now();
    try{
        var i = 0;
        for (i = 0; i < VUS; i++) {
            const username = `${PREFIX}${runId}+${i}@example.com`;
            var body = JSON.stringify({
                UserAttributes: [
                    { Name : 'email', Value: username },
                    { Name: 'email_verified', Value: 'true' }
                ],
                UserPoolId: 'ap-northeast-1_u6Y87N8S1',
                'Username': username, 
                'MessageAction': 'SUPPRESS',
                
            })
            let response = callCognito(signer, 'AdminCreateUser', body);
            if (response.status !== 200) {
                throw new Error(`Failed to 'AdminCreateUser': ${response.status} ${response.body}`);
            }
            // 認証情報の渡し方
            // パスワード
            body = JSON.stringify({
                UserPoolId: 'ap-northeast-1_u6Y87N8S1',
                'Username': username, 
                'Password': PASSWORD,
                'Permanent': true
            })
            response = callCognito(signer, 'AdminSetUserPassword', body);
            if (response.status !== 200) {
                throw new Error(`Failed to 'AdminSetUserPassword': ${response.status} ${response.body}`);
            }
            
            body = JSON.stringify({
                AuthFlow: 'USER_PASSWORD_AUTH',
                ClientId: '2i3d508aqfla13dmksip64s5sv',
                AuthParameters: {
                    USERNAME: username,
                    PASSWORD: PASSWORD
                }
            })
            response = http.post(
                'https://cognito-idp.ap-northeast-1.amazonaws.com/',
                body,
                {headers:{
                    'Content-Type': 'application/x-amz-json-1.1',
                    'X-Amz-Target': 'AWSCognitoIdentityProviderService.InitiateAuth'
                }})
                
                if (response.status !== 200) {
                    throw new Error(`Failed to login test user: ${response.status} ${response.body}`);
                }
                // setup() の制限時間
                tokens.push(response.json().AuthenticationResult.IdToken);
            }
        }
        catch (err) {
            deleteTestUsers(signer, runId, i+1);  // 作成済みのユーザーを削除
            console.error(`Error during setup: ${err.message}`);
            throw err;  // エラーを再スローしてテストを失敗させる
        }

        return {runId: runId, questId: `load-test-${runId}`, tokens: tokens};
    }

function callCognito(signer, target, body){
    const signedRequest = signer.sign({
        method: 'POST',
        endpoint: new Endpoint('https://cognito-idp.ap-northeast-1.amazonaws.com'), 
        path: '/',
        headers: {
            'Content-Type': 'application/x-amz-json-1.1', 
            'X-Amz-Target': `AWSCognitoIdentityProviderService.${target}`
        }, 
        body: body
    });

    const response = http.post(signedRequest.url, body, { headers: signedRequest.headers });
    return response;
}

function createSigner(){
    return new SignatureV4({
        service: 'cognito-idp',
        region: 'ap-northeast-1',
        credentials: {
            accessKeyId: __ENV.AWS_ACCESS_KEY_ID,
            secretAccessKey: __ENV.AWS_SECRET_ACCESS_KEY,
            sessionToken: __ENV.AWS_SESSION_TOKEN,
        },
        uriEscapePath: true,
    });
}

function deleteTestUsers(signer, runId, count) {
    for (let i = 0; i < count; i++) {
        const username = `${PREFIX}${runId}+${i}@example.com`;
        const body = JSON.stringify({
            UserPoolId: 'ap-northeast-1_u6Y87N8S1',
            Username: username
        });
        const response = callCognito(signer, 'AdminDeleteUser', body);
        if (response.status !== 200) {
            console.error(`Failed to 'AdminDeleteUser' ${username}: ${response.status} ${response.body}`);
        }
    }
}

export function teardown(data) {
    // 後片付けの処理
    // 例: テストユーザーの削除
    const signer = createSigner();
    const runId = data.runId;
    deleteTestUsers(signer, runId, data.tokens.length);
}

const matched = new Rate('matched');
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
}