// 调试面板（FR-7 / PRD §4.8）：1Hz 实时数据（手势状态/触摸采样率/发送速率/RTT/收发计数）、
// echo 自测（10 连发列 RTT）、手动连接区（M1 联调：IP/端口/token(hex) + 连接/断开，
// 交互四态：空闲/连接中(LoadingProgress)/成功/失败(带原因)+断开提示；成功后写 lastHost 偏好）。
import type { BusinessError } from '@kit.BasicServicesKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { GestureStateName } from '../common/gesture/GestureEngine';
import { ConnState } from '../common/net/ConnectionManager';
import type { ConnectionManager, RttListener } from '../common/net/ConnectionManager';
import { hexToBytes } from '../common/net/HexCodec';
import type { TouchpadModel } from '../common/viewmodel/TouchpadModel';
import type { SettingsModel } from '../common/viewmodel/SettingsModel';
import { HostInfo } from '../common/viewmodel/SettingsModel';
import { HostsFullError } from '../common/viewmodel/HostsStore';
import { pageResStr } from './Res';

const TAG: string = 'PadLink.Debug';
const DOMAIN: number = 0x5050;
const ECHO_TEST_COUNT: number = 10;

// 合成轨迹参数（FR-7）：72 点近似圆、半径 120 计数、8ms 步进（约 0.6s 画完）。
const TRAJ_SEGMENTS: number = 72;
const TRAJ_RADIUS: number = 120;
const TRAJ_STEP_MS: number = 8;

// GestureStateName（0..7）→ 文案资源（$r 需字面量，不用拼名）。
function gestureRes(state: number): string {
  if (state === GestureStateName.SINGLE_PENDING) {
    return 'gesture_single_pending';
  }
  if (state === GestureStateName.CURSOR) {
    return 'gesture_cursor';
  }
  if (state === GestureStateName.DRAGGING) {
    return 'gesture_dragging';
  }
  if (state === GestureStateName.HOLD_MULTI) {
    return 'gesture_hold_multi';
  }
  if (state === GestureStateName.TWO_PENDING) {
    return 'gesture_two_pending';
  }
  if (state === GestureStateName.SCROLLING) {
    return 'gesture_scrolling';
  }
  if (state === GestureStateName.THREE_PENDING) {
    return 'gesture_three_pending';
  }
  return 'gesture_idle';
}

function connRes(state: number): string {
  if (state === ConnState.CONNECTED) {
    return 'state_connected';
  }
  if (state === ConnState.RECONNECTING) {
    return 'state_reconnecting';
  }
  if (state === ConnState.LOST) {
    return 'state_lost';
  }
  return 'state_idle';
}

// hex 字符串 → 字节：见 common/net/HexCodec（与 M2 配对/快速连接共用）。

// statRow 入参（@Builder 按引用传递的唯一参数）：label/value 随 @Local/@Trace 变化刷新。
// 依据 arkts-builder：@Builder 多参/按值传递状态变量时不触发动态渲染（faqs-arkui-1078），
// 仅「单参数 + 对象字面量」按引用传递才随状态变化刷新。
class StatRowData {
  public label: ResourceStr = '';
  public value: string = '';
}

@ComponentV2
export struct DebugPanelPage {
  @Require @Param conn: ConnectionManager;
  @Require @Param touchpad: TouchpadModel;
  @Require @Param settings: SettingsModel;
  @Require @Param pathStack: NavPathStack;

  // 实时数据（1Hz 刷新）
  @Local gestureState: number = GestureStateName.IDLE;
  @Local sampleRate: number = 0;
  @Local sendRate: number = 0;
  @Local rttMs: number = -1;
  @Local udpSendOk: number = 0;
  @Local udpSendFail: number = 0;
  @Local tcpSendOk: number = 0;
  @Local tcpSendFail: number = 0;
  @Local recvOk: number = 0;
  @Local recvDecodeFail: number = 0;
  @Local recvHmacFail: number = 0;

  // echo 自测
  @Local echoRunning: boolean = false;
  @Local echoRtts: number[] = [];

  // 合成轨迹自测（FR-7）：发送圆形 MOVE 序列，验证 UDP 数据面到 daemon 注入的端到端链路
  @Local trajRunning: boolean = false;
  private trajIdx: number = 0;
  private trajTimer: number = -1;

  // 手动连接区（四态：空闲/loading/成功/失败；断开提示复用 statusText）
  @Local ip: string = '';
  @Local portText: string = '53021';
  @Local tokenHex: string = '';
  @Local connecting: boolean = false;
  @Local statusText: string = '';
  @Local statusOk: boolean = false;
  // 页面存活秒数（1Hz 走字）：既是 Realtime 区刷新节拍的可视证据，也用于排查整页渲染冻结
  @Local pageSec: number = 0;
  private refreshTimer: number = -1;
  private openedAtMs: number = 0;
  private echoPending: number = 0;
  private rttCb: RttListener = (rtt: number) => {
    this.onRtt(rtt);
  };

  aboutToAppear(): void {
    this.conn.addRttListener(this.rttCb);
    this.openedAtMs = new Date().getTime();
    this.settings.getLastHost().then((h: HostInfo | null) => {
      if (h !== null) {
        this.ip = h.ip;
        this.portText = `${h.port}`;
        this.tokenHex = h.tokenHex;
      }
    }).catch((err: BusinessError) => {
      hilog.warn(DOMAIN, TAG, 'getLastHost fail: %{public}s', err.message);
    });
    this.refreshNow();
    this.refreshTimer = setInterval(() => {
      this.refreshNow();
    }, 1000);
  }

  aboutToDisappear(): void {
    if (this.refreshTimer >= 0) {
      clearInterval(this.refreshTimer);
      this.refreshTimer = -1;
    }
    if (this.trajTimer >= 0) {
      clearInterval(this.trajTimer);
      this.trajTimer = -1;
      this.trajRunning = false;
    }
    this.conn.removeRttListener(this.rttCb);
  }

  private refreshNow(): void {
    this.pageSec = Math.floor((Date.now() - this.openedAtMs) / 1000);
    this.gestureState = this.touchpad.currentState();
    this.sampleRate = this.touchpad.getSampleRate();
    this.sendRate = this.touchpad.getSendRate();
    this.rttMs = this.conn.snapshot.rttMs;
    const st = this.conn.transport.stats;
    this.udpSendOk = st.udpSendOk;
    this.udpSendFail = st.udpSendFail;
    this.tcpSendOk = st.tcpSendOk;
    this.tcpSendFail = st.tcpSendFail;
    this.recvOk = st.recvOk;
    this.recvDecodeFail = st.recvDecodeFail;
    this.recvHmacFail = st.recvHmacFail;
  }

  private onRtt(rtt: number): void {
    if (this.echoPending > 0) {
      this.echoPending--;
      const list: number[] = this.echoRtts.slice();
      list.push(rtt);
      this.echoRtts = list;
      if (this.echoPending === 0) {
        this.echoRunning = false;
      }
    }
  }

  private runEchoTest(): void {
    if (this.echoRunning) {
      return;
    }
    if (!this.conn.isConnected()) {
      this.showStatus(this.resStr('debug_need_connect'), false);
      return;
    }
    this.echoRtts = [];
    this.echoPending = ECHO_TEST_COUNT;
    this.echoRunning = true;
    for (let i = 0; i < ECHO_TEST_COUNT; i++) {
      this.conn.transport.sendEcho();
    }
  }

  // 合成轨迹：相邻圆周点差分为相对位移（净位移为零，光标画圆后回原位），
  // 8ms 间隔逐点发送（约 0.6s 画完，肉眼可辨），替代真机 --test 的链路层验证。
  private runTrajectoryTest(): void {
    if (this.trajRunning) {
      return;
    }
    if (!this.conn.isConnected()) {
      this.showStatus(this.resStr('debug_need_connect'), false);
      return;
    }
    this.trajIdx = 0;
    this.trajRunning = true;
    let prevX: number = TRAJ_RADIUS;
    let prevY: number = 0;
    this.trajTimer = setInterval(() => {
      this.trajIdx++;
      const a: number = 2 * Math.PI * this.trajIdx / TRAJ_SEGMENTS;
      const x: number = Math.round(TRAJ_RADIUS * Math.cos(a));
      const y: number = Math.round(TRAJ_RADIUS * Math.sin(a));
      this.conn.transport.sendMove(Math.trunc(x - prevX), Math.trunc(y - prevY));
      prevX = x;
      prevY = y;
      if (this.trajIdx >= TRAJ_SEGMENTS) {
        clearInterval(this.trajTimer);
        this.trajTimer = -1;
        this.trajRunning = false;
        this.showStatus(this.resStr('debug_traj_done'), true);
      }
    }, TRAJ_STEP_MS);
  }

  private showStatus(text: string, ok: boolean): void {
    this.statusText = text;
    this.statusOk = ok;
  }

  private async onConnectClick(): Promise<void> {
    if (this.connecting) {
      return;
    }
    const ip: string = this.ip.trim();
    const port: number = parseInt(this.portText.trim(), 10);
    if (ip.length === 0 || Number.isNaN(port) || port <= 0 || port > 65535) {
      this.showStatus(this.resStr('debug_input_invalid'), false);
      return;
    }
    const token: Uint8Array | null = hexToBytes(this.tokenHex.trim());
    if (token === null || token.length !== 32) {
      this.showStatus(this.resStr('debug_token_invalid'), false);
      return;
    }
    this.connecting = true; // loading 态：按钮禁用 + LoadingProgress
    this.showStatus(this.resStr('debug_connecting'), false);
    try {
      await this.conn.connect(ip, port, port, token);
      this.showStatus(this.resStr('debug_connected'), true); // 成功态
      const h = new HostInfo();
      h.ip = ip;
      h.port = port;
      h.tokenHex = this.tokenHex.trim();
      h.name = ip;
      this.settings.saveLastHost(h).catch((err: Error) => {
        // 保存失败不影响本会话，状态行给原因（满 4 台给「先删除」提示，可删除后重试保存）
        if (err instanceof HostsFullError) {
          this.showStatus(this.resStr('debug_hosts_full'), false);
          return;
        }
        this.showStatus(`${this.resStr('debug_save_fail')}（${err.message}）`, false);
        hilog.warn(DOMAIN, TAG, 'saveLastHost fail: %{public}s', err.message);
      });
    } catch (err) {
      this.showStatus((err as Error).message, false); // 失败态：给原因，可重试
    } finally {
      this.connecting = false;
    }
  }

  private onDisconnectClick(): void {
    this.conn.disconnect();
    this.showStatus(this.resStr('debug_disconnected'), true); // 断开提示
  }

  private rttText(): string {
    return this.rttMs >= 0 ? `${this.rttMs} ms` : '--';
  }

  private resStr(name: string): string {
    return pageResStr(this.getUIContext(), name);
  }

  @Builder
  statRow($$: StatRowData) {
    Row() {
      Text($$.label)
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor($r('sys.color.font_secondary'))
      Blank()
      Text($$.value)
        .fontSize($r('sys.float.ohos_id_text_size_body2'))
        .fontColor($r('sys.color.font_primary'))
    }
    .width('100%')
  }

  build() {
    NavDestination() {
      Scroll() {
        Column({ space: 16 }) {
          // ---- 实时数据（1Hz） ----
          Column({ space: 8 }) {
            Row() {
              Text($r('app.string.debug_realtime'))
                .fontSize($r('sys.float.ohos_id_text_size_body1'))
                .fontColor($r('sys.color.font_primary'))
              Blank()
              Text(`t+${this.pageSec}s`)
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor($r('sys.color.font_secondary'))
            }
            .width('100%')
            this.statRow({ label: $r('app.string.debug_conn_state'), value: this.resStr(connRes(this.conn.snapshot.state)) })
            this.statRow({ label: $r('app.string.debug_gesture_state'), value: this.resStr(gestureRes(this.gestureState)) })
            this.statRow({ label: $r('app.string.debug_sample_rate'), value: `${this.sampleRate}/s` })
            this.statRow({ label: $r('app.string.debug_send_rate'), value: `${this.sendRate}/s` })
            this.statRow({ label: $r('app.string.debug_rtt'), value: this.rttText() })
            this.statRow({ label: $r('app.string.debug_udp_fail'), value: `${this.udpSendFail}` })
            this.statRow({ label: $r('app.string.debug_tcp_decode_fail'), value: `${this.recvDecodeFail}` })
            this.statRow({ label: $r('app.string.debug_hmac_fail'), value: `${this.recvHmacFail}` })
            this.statRow({ label: $r('app.string.debug_udp_ok'), value: `${this.udpSendOk}` })
            this.statRow({ label: $r('app.string.debug_tcp_ok'), value: `${this.tcpSendOk}` })
            this.statRow({ label: $r('app.string.debug_recv_ok'), value: `${this.recvOk}` })
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')

          Divider().color($r('sys.color.comp_divider'))

          // ---- echo 自测 ----
          Column({ space: 8 }) {
            Row() {
              Text($r('app.string.debug_echo_test'))
                .fontSize($r('sys.float.ohos_id_text_size_body1'))
                .fontColor($r('sys.color.font_primary'))
              Blank()
              Button($r('app.string.debug_echo_run'))
                .onClick(() => {
                  this.runEchoTest();
                })
            }
            .width('100%')
            if (this.echoRtts.length > 0) {
              ForEach(this.echoRtts, (rtt: number, idx: number) => {
                Text(`#${idx + 1}: ${rtt} ms`)
                  .fontSize($r('sys.float.ohos_id_text_size_body2'))
                  .fontColor($r('sys.color.font_secondary'))
              }, (rtt: number, idx: number) => `echo-${idx}-${rtt}`)
            }
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')

          Divider().color($r('sys.color.comp_divider'))

          // ---- 合成轨迹自测（FR-7：验证 UDP 数据面到 daemon 注入端到端） ----
          Row() {
            Text($r('app.string.debug_traj_test'))
              .fontSize($r('sys.float.ohos_id_text_size_body1'))
              .fontColor($r('sys.color.font_primary'))
            Blank()
            Button($r('app.string.debug_traj_run'))
              .enabled(!this.trajRunning)
              .onClick(() => {
                this.runTrajectoryTest();
              })
            if (this.trajRunning) {
              LoadingProgress()
                .width(24)
                .height(24)
                .color($r('sys.color.brand'))
            }
          }
          .width('100%')

          Divider().color($r('sys.color.comp_divider'))

          // ---- 手动连接区（M1 联调） ----
          Column({ space: 8 }) {
            Text($r('app.string.debug_manual_connect'))
              .fontSize($r('sys.float.ohos_id_text_size_body1'))
              .fontColor($r('sys.color.font_primary'))
            TextInput({ text: this.ip, placeholder: this.resStr('debug_ip_hint') })
              .height(40)
              .backgroundColor($r('sys.color.comp_background_tertiary'))
              .onChange((v: string) => {
                this.ip = v;
              })
            TextInput({ text: this.portText, placeholder: this.resStr('debug_port_hint') })
              .height(40)
              .backgroundColor($r('sys.color.comp_background_tertiary'))
              .onChange((v: string) => {
                this.portText = v;
              })
            TextInput({ text: this.tokenHex, placeholder: this.resStr('debug_token_hint') })
              .height(40)
              .backgroundColor($r('sys.color.comp_background_tertiary'))
              .onChange((v: string) => {
                this.tokenHex = v;
              })
            Row({ space: 12 }) {
              Button(this.connecting ? $r('app.string.debug_connecting') : $r('app.string.debug_connect'))
                .enabled(!this.connecting)
                .onClick(() => {
                  this.onConnectClick();
                })
              Button($r('app.string.debug_disconnect'))
                .buttonStyle(ButtonStyleMode.NORMAL)
                .onClick(() => {
                  this.onDisconnectClick();
                })
              if (this.connecting) {
                LoadingProgress()
                  .width(24)
                  .height(24)
                  .color($r('sys.color.brand'))
              }
            }
            if (this.statusText.length > 0) {
              Text(this.statusText)
                .fontSize($r('sys.float.ohos_id_text_size_body2'))
                .fontColor(this.statusOk ? $r('sys.color.font_primary') : $r('sys.color.font_emphasize'))
            }
          }
          .alignItems(HorizontalAlign.Start)
          .width('100%')
        }
        .padding(16)
        .width('100%')
      }
      .width('100%')
      .height('100%')
      .scrollBar(BarState.Off)
    }
    .title($r('app.string.page_debug'))
  }
}
