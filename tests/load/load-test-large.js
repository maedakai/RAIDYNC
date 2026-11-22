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
// 最初の iteration を VU ごとにずらす間隔（秒）。0.1 なら 1 秒に 10 本ずつ新しい接続を作る（1,000 VU で 100 秒）。
// 一斉に接続すると、PC 側の経路で最初の POST がタイムアウトする。1 秒に 20 本（0.05）でも 1,000 VU では
// 数百本目から失敗したため、作る速さを落としている（docs/qa-log.md）。
const PERIOD = Number(__ENV.PERIOD || 0.1)

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
    // 不具合そのものにだけしきい値を置く。matched は終わり際に相手がそろわない TIMEOUT が必ず出るので
    // 記録だけにし、正しさは server/cmd/sessions でセッション人数の合計と照らし合わせる（docs/qa-log.md）。
    thresholds: {
        http_req_failed: ['rate==0'],
        matchFailed: ['rate==0'],
        matchStuck: ['rate==0'],
    },
    setupTimeout: '20m',  // setup() の制限時間を 20 分に設定
};

export function setup() {
    if(!API_URL){
        throw new Error('API_URL environment variable is not set');
    }
    // 4 人組でマッチするので、4 の倍数でないと全体のチケット数が割り切れない
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
    const questId = `load-test-${runId}`;
    console.log(questId);
    return {questId: questId, tokens: tokens};
}

const matched = new Rate('matched');
const matchFailed = new Rate('matchFailed')
// 締め切りまで終わりの状態（MATCHED / TIMEOUT / FAILED）にならなかった割合。MATCHING のまま止まる不具合を捕まえる
const matchStuck = new Rate('matchStuck')
export default function(data) {
    // 最初の 1 回だけ待つ。2 回目以降は接続を使い回すので一斉接続にならない。
    // この待ちは最初の iteration_duration に含まれる（マッチの遅さは http_req_duration で見る）
    if(exec.vu.iterationInScenario === 0){
        sleep((exec.vu.idInTest-1)*PERIOD);
    }
    var response = http.post(`${API_URL}/match/request`, 
        JSON.stringify({'questId': data.questId}),
        { headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${data.tokens[exec.vu.idInTest - 1]}` } });
    check(response, { 'request is 200': (r) => r.status === 200 })
    if(response.status !== 200){
        matched.add(false);
        return;
    }
    // 締め切りは POST が成功してから数える。サーバーの見切り（作成から 60 秒）より必ず 10 秒以上あとになり、
    // 接続に時間がかかっても k6 が先に諦めない（docs/qa-log.md）
    const deadline = Date.now() + 70 * 1000;
    const ticketId = response.json().ticketId;
    const createdAt = response.json().createdAt;
    let state = '';
    while (state !== 'MATCHED' && state !== 'TIMEOUT'&&state!== 'FAILED'&& Date.now() < deadline) {
        sleep(2);
        response = http.get(`${API_URL}/match/status/${ticketId}`,
        {headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${data.tokens[exec.vu.idInTest - 1]}` },tags: { name: 'match/status/{ticketId}'} });
        if(response.status !== 200){
            continue;
        }
        state = response.json().status;
    }
    if(state !== 'MATCHED' && state !== 'TIMEOUT' && state !== 'FAILED'){
        console.warn(`stuck: ticketId=${ticketId} state=${state} age=${Date.now()-createdAt}ms lastStatus=${response.status}`) 
    }
    matched.add(state === 'MATCHED');
    matchFailed.add(state === 'FAILED');
    matchStuck.add(state !== 'MATCHED' && state !== 'TIMEOUT' && state !== 'FAILED');
}