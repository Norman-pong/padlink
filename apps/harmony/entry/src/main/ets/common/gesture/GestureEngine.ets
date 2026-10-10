// 触摸板手势判定状态机（PRD §4.2 手势表；单页融合后功能区切换归浮动按钮，三指仅存吸收态）。
// 纯被动引擎：不取时钟、不持定时器，超时判定在样本到达时用 tMs 差惰性比较；
// 任何分支都有复位路径（CANCEL / 全部抬起 → 释放按键、冲账滚动、清状态回 Idle）。
// 纯逻辑层：禁止 @ohos 依赖、禁止装饰器、不用 TS enum（保证 node 可直跑）。
import { toCounts } from './Accel';
import type { AccelResult } from './Accel';
import type { GestureConfig } from './GestureConfig';
import { TouchKind } from './TouchSample';
import type { TouchSample } from './TouchSample';

// 命令类别。
export class GestureCommandType {
  public static readonly CURSOR_MOVE: number = 0;
  public static readonly BUTTON: number = 1;
  public static readonly SCROLL: number = 2;
}

// 按钮 code（与协议 BUTTON btn 一致：1=左 3=右）。
export class HidButton {
  public static readonly LEFT: number = 1;
  public static readonly RIGHT: number = 3;
}

// 引擎输出命令：一条样本可产生 0..N 条；字段按 type 取用（其余为零值，同 Packet 风格）。
export interface GestureCommand {
  type: number;
  dx: number; // CURSOR_MOVE：计数
  dy: number; // CURSOR_MOVE：计数
  btn: number; // BUTTON：1 左 / 3 右
  down: boolean; // BUTTON：按下/抬起
  dyHiRes: number; // SCROLL：HI_RES 单位
}

export function cmdCursorMove(dx: number, dy: number): GestureCommand {
  const c: GestureCommand = { type: GestureCommandType.CURSOR_MOVE, dx: dx, dy: dy, btn: 0, down: false, dyHiRes: 0 };
  return c;
}

export function cmdButton(btn: number, down: boolean): GestureCommand {
  const c: GestureCommand = { type: GestureCommandType.BUTTON, dx: 0, dy: 0, btn: btn, down: down, dyHiRes: 0 };
  return c;
}

export function cmdScroll(dyHiRes: number): GestureCommand {
  const c: GestureCommand = { type: GestureCommandType.SCROLL, dx: 0, dy: 0, btn: 0, down: false, dyHiRes: dyHiRes };
  return c;
}

// 状态机状态。注释含各状态的进入/退出条件。
export class GestureStateName {
  // 无接触。
  // 进入：任何复位路径（手势完成、CANCEL、全部抬起）。
  // 退出：首个 DOWN → SINGLE_PENDING。
  public static readonly IDLE: number = 0;
  // 单指按下待定型：轻点/长按候选（未超 150ms、位移未超 8vp）。
  // 进入：IDLE 下单指 DOWN（若距上次轻点抬起 ≤300ms 且同位置 → 标记双击后拖拽候选）。
  // 退出：UP ≤150ms 且 ≤8vp → 成对发左键 down+up 回 IDLE；UP 在 150..300ms 之间 → 无命令回 IDLE；
  //       惰性到达 ≥300ms 且 ≤8vp → DRAGGING（发左键 down）；
  //       位移超 8vp → 双击后拖拽候选则 DRAGGING（发左键 down，双击后拖拽），否则 CURSOR；
  //       第二指 DOWN → TWO_PENDING；CANCEL → IDLE。
  public static readonly SINGLE_PENDING: number = 1;
  // 单指光标移动（轻点/长按已出局，无按键按下）。窗口内（≤8vp）的移动不发光标，越界样本起才发。
  // 进入：SINGLE_PENDING 位移超 8vp。
  // 退出：UP → IDLE；第二指 DOWN → TWO_PENDING；CANCEL → IDLE。
  public static readonly CURSOR: number = 2;
  // 长按拖拽：引擎持有左键按下（down 已发）。
  // 进入：SINGLE_PENDING 惰性到达 ≥300ms 且位移 ≤8vp。
  // 退出：UP → 发左键 up 回 IDLE；第二指 DOWN → HOLD_MULTI（按键保持）；CANCEL → 发左键 up 回 IDLE。
  public static readonly DRAGGING: number = 3;
  // 拖拽中额外手指按下：左键保持，忽略一切移动，全部抬起时才释放左键。
  // 进入：DRAGGING 中第二指 DOWN。
  // 退出：全部抬起 → 发左键 up 回 IDLE；CANCEL 同。
  public static readonly HOLD_MULTI: number = 4;
  // 双指按下待判定：右键轻点候选 / 滚动前夜。
  // 进入：单指态（SINGLE_PENDING/CURSOR）遇第二指 DOWN（两指按下时间差 ≤40ms 才有轻点资格）；
  //       THREE_PENDING 存活指 <3 时回落（无轻点资格，起点重置）。
  // 退出：双指均 ≤150ms 且 ≤8vp 抬起且有时间差资格 → 成对发右键 down+up 回 IDLE；
  //       任一指位移超 8vp 或轻点窗口过期后有移动 → SCROLLING；CANCEL → IDLE。
  public static readonly TWO_PENDING: number = 5;
  // 滚动（双指；滚动中一指抬起后余一指继续）。垂直分量累积映射 HI_RES，结束/接管前冲账余量（不丢尾）。
  // 进入：TWO_PENDING 判滚（超位移或窗口过期后移动）。
  // 退出：末指 UP → 冲账 Scroll 回 IDLE；第三指 DOWN → 冲账后 THREE_PENDING；CANCEL → 冲账回 IDLE。
  public static readonly SCROLLING: number = 6;
  // ≥3 指存活：吸收一切输入、无任何输出（防手掌/多指误触——三指切换手势已随单页融合删除，
  // 本态只保留抑制语义；SCROLLING 进入前先冲账，DRAGGING/HOLD_MULTI 不进入，转 HOLD_MULTI 保持按键）。
  // 进入：任一状态存活指达 3。
  // 退出：存活指 <3 → 回落 TWO_PENDING（起点重置、无轻点资格）；CANCEL → IDLE。
  public static readonly THREE_PENDING: number = 7;
}

// 指针跟踪记录（引擎内部）。
interface PointerTrack {
  startX: number; // 本段判定的起点（多指接管/回落时重置）
  startY: number;
  lastX: number;
  lastY: number;
  downTMs: number; // 本段起点时间
  lastTMs: number;
}

// 滚动速度估计的指数平滑系数（越大越跟手、越小越平滑；真机手感待校准）。
const SCROLL_VELOCITY_ALPHA: number = 0.35;

export class GestureEngine {
  private cfg: GestureConfig;
  private state: number = GestureStateName.IDLE;
  private pointers: Map<number, PointerTrack> = new Map();
  private primaryId: number = -1; // 首指（轻点窗口/按下时间差的基准）
  private tapCandidate: boolean = false; // 双指轻点资格（按下时间差 ≤40ms）
  private tapFirstLiftOk: boolean = false; // 双指中先抬指的轻点资格
  private buttonHeld: boolean = false; // 引擎持有的左键（长按拖拽 / 双击后拖拽）
  private cursorResX: number = 0; // 光标亚像素残差
  private cursorResY: number = 0;
  private scrollAccUnits: number = 0; // 滚动累积器（HI_RES 单位，含小数）
  // 双击后拖拽：上一次轻点抬起的时间与位置 + 本次按下是否构成拖拽候选
  private lastTapUpTMs: number = 0;
  private lastTapUpX: number = 0;
  private lastTapUpY: number = 0;
  private tapDragArmed: boolean = false;
  // 抬指惯性滑行（动量滚动）：速度估计与状态
  private scrollVelVp: number = 0; // 滚动速度估计（vp/ms，指数平滑）
  private flingV: number = 0; // 惯性初速（vp/ms；0 = 无惯性）
  private flingElapsedMs: number = 0; // 惯性已滑行时长

  constructor(cfg: GestureConfig) {
    this.cfg = cfg;
  }

  // 当前状态（调试面板展示 / 单测断言用）。
  public currentState(): number {
    return this.state;
  }

  // 是否处于惯性滑行（调用方据此驱动 tick；引擎自身不持时钟/定时器）。
  public hasFling(): boolean {
    return this.flingV !== 0;
  }

  // 惯性滑行推进 dtMs 毫秒，返回本次应下发的滚动命令。
  // 位移按一阶衰减积分：Δy = v·τ·(1 - e^(-dt/τ))，v ← v·e^(-dt/τ)（自然滚动符号同滚动路径）。
  public tick(dtMs: number): GestureCommand[] {
    const out: GestureCommand[] = [];
    if (this.flingV === 0 || dtMs <= 0) {
      return out;
    }
    this.flingElapsedMs += dtMs;
    if (this.flingElapsedMs >= this.cfg.flingMaxMs) {
      this.stopFling(out);
      return out;
    }
    const decay: number = Math.exp(-dtMs / this.cfg.flingTauMs);
    const dyVp: number = this.flingV * this.cfg.flingTauMs * (1 - decay);
    this.flingV *= decay;
    this.scrollByVp(dyVp, out);
    if (Math.abs(this.flingV) <= this.cfg.flingStopVpPerMs) {
      this.stopFling(out); // 速度衰减到阈值以下：冲账余量收尾
    }
    return out;
  }

  // 注入一条样本，返回 0..N 条命令。
  public feed(s: TouchSample): GestureCommand[] {
    const out: GestureCommand[] = [];
    if (s.kind === TouchKind.CANCEL) {
      this.cancelAll(out); // 任一指 CANCEL 视为全局取消
      return out;
    }
    if (s.kind === TouchKind.DOWN) {
      this.onDown(s, out);
    } else if (s.kind === TouchKind.MOVE) {
      this.onMove(s, out);
    } else if (s.kind === TouchKind.UP) {
      this.onUp(s, out);
    }
    return out;
  }

  private onDown(s: TouchSample, out: GestureCommand[]): void {
    if (this.pointers.has(s.pointerId)) {
      return; // 重复 DOWN 忽略
    }
    // 任何新按下都终止惯性滑行（触停语义），尾量冲账不丢
    if (this.flingV !== 0) {
      this.stopFling(out);
    }
    const tr: PointerTrack = { startX: s.x, startY: s.y, lastX: s.x, lastY: s.y, downTMs: s.tMs, lastTMs: s.tMs };
    if (this.state === GestureStateName.IDLE) {
      this.pointers.set(s.pointerId, tr);
      this.primaryId = s.pointerId;
      this.state = GestureStateName.SINGLE_PENDING;
      // 双击后拖拽候选：距上次轻点抬起 ≤300ms 且落点几乎相同（双击的第二下按住拖动）
      this.tapDragArmed = this.lastTapUpTMs > 0
        && s.tMs - this.lastTapUpTMs <= this.cfg.doubleTapDragMs
        && Math.hypot(s.x - this.lastTapUpX, s.y - this.lastTapUpY) <= this.cfg.tapMaxVp;
      return;
    }
    if (this.state === GestureStateName.DRAGGING) {
      // 长按已按下左键：按键保持，转 HOLD_MULTI，全部抬起时释放
      this.pointers.set(s.pointerId, tr);
      this.state = GestureStateName.HOLD_MULTI;
      return;
    }
    if (this.state === GestureStateName.HOLD_MULTI || this.state === GestureStateName.THREE_PENDING) {
      this.pointers.set(s.pointerId, tr); // 仅记录，不改变当前判定
      return;
    }
    // SINGLE_PENDING / CURSOR / TWO_PENDING / SCROLLING 上的新指
    this.pointers.set(s.pointerId, tr);
    if (this.state === GestureStateName.SCROLLING) {
      this.flushScroll(out); // 多指接管前冲账滚动余量，不丢尾
    }
    if (this.pointers.size >= 3) {
      // 第三指插入：取消此前一切未定型判定（尚无按键按下，无命令可发），转多指吸收态
      this.state = GestureStateName.THREE_PENDING;
      this.tapCandidate = false;
      this.tapFirstLiftOk = false;
      return;
    }
    // 第二指插入：取消单指一切判定（未完成命令不发；残差清零防漂移进新判定）
    this.state = GestureStateName.TWO_PENDING;
    this.cursorResX = 0;
    this.cursorResY = 0;
    const primary: PointerTrack | undefined = this.pointers.get(this.primaryId);
    const gap: number = primary === undefined ? 0 : Math.abs(s.tMs - primary.downTMs);
    this.tapCandidate = gap <= this.cfg.twoFingerTapMaxMs;
    this.tapFirstLiftOk = false;
  }

  private onMove(s: TouchSample, out: GestureCommand[]): void {
    const tr: PointerTrack | undefined = this.pointers.get(s.pointerId);
    if (tr === undefined) {
      return; // 非活跃指的 MOVE 直接忽略（禁止光标漂移）
    }
    const prevX: number = tr.lastX;
    const prevY: number = tr.lastY;
    const prevT: number = tr.lastTMs;
    tr.lastX = s.x;
    tr.lastY = s.y;
    tr.lastTMs = s.tMs;
    if (this.state === GestureStateName.SINGLE_PENDING) {
      const disp: number = Math.hypot(tr.lastX - tr.startX, tr.lastY - tr.startY);
      if (disp > this.cfg.tapMaxVp) {
        if (this.tapDragArmed) {
          // 双击后拖拽：第二次按下后开始移动 → 按住左键拖拽（daemon 侧合成 clickCount=2 的拖拽）
          this.tapDragArmed = false;
          this.state = GestureStateName.DRAGGING;
          this.buttonHeld = true;
          out.push(cmdButton(HidButton.LEFT, true));
          return;
        }
        this.state = GestureStateName.CURSOR; // 超位移：轻点/长按出局，越界样本起发光标
        this.emitCursor(prevX, prevY, prevT, s, out);
      } else if (s.tMs - tr.downTMs >= this.cfg.longPressMs) {
        // 惰性长按：位移未超且已到 300ms（无后台定时器，靠样本时间差）
        this.state = GestureStateName.DRAGGING;
        this.buttonHeld = true;
        out.push(cmdButton(HidButton.LEFT, true));
      }
      return;
    }
    if (this.state === GestureStateName.CURSOR || this.state === GestureStateName.DRAGGING) {
      this.emitCursor(prevX, prevY, prevT, s, out);
      return;
    }
    if (this.state === GestureStateName.HOLD_MULTI) {
      return; // 按键保持期间忽略移动
    }
    if (this.state === GestureStateName.TWO_PENDING) {
      if (this.anyPointerOverTapDist() || this.tapWindowExpired(s.tMs)) {
        this.state = GestureStateName.SCROLLING; // 判滚：右键轻点出局
        this.tapCandidate = false;
        this.accumulateScroll(s.y - prevY, s.tMs - prevT, out);
      }
      return;
    }
    if (this.state === GestureStateName.SCROLLING) {
      this.accumulateScroll(s.y - prevY, s.tMs - prevT, out);
      return;
    }
    // THREE_PENDING：多指吸收态，移动不产生任何输出。
  }

  private onUp(s: TouchSample, out: GestureCommand[]): void {
    const tr: PointerTrack | undefined = this.pointers.get(s.pointerId);
    if (tr === undefined) {
      return; // 非活跃指 UP 忽略
    }
    tr.lastX = s.x;
    tr.lastY = s.y;
    tr.lastTMs = s.tMs;
    if (this.state === GestureStateName.SINGLE_PENDING) {
      const dt: number = s.tMs - tr.downTMs;
      const disp: number = Math.hypot(tr.lastX - tr.startX, tr.lastY - tr.startY);
      this.pointers.delete(s.pointerId);
      this.toIdle();
      if ((dt <= this.cfg.tapMaxMs || dt >= this.cfg.longPressMs) && disp <= this.cfg.tapMaxVp) {
        // 轻点（≤150ms）或长按后原地抬起（≥300ms）：左键 down+up 成对补发
        out.push(cmdButton(HidButton.LEFT, true));
        out.push(cmdButton(HidButton.LEFT, false));
        if (dt <= this.cfg.tapMaxMs) {
          // 记录本次轻点抬起：下一次按下落在窗口内即构成双击（点击或"双击后拖拽"）
          this.lastTapUpTMs = s.tMs;
          this.lastTapUpX = s.x;
          this.lastTapUpY = s.y;
        }
      }
      // 150..300ms 之间抬起：既非轻点也未成长按，无命令
      return;
    }
    if (this.state === GestureStateName.CURSOR) {
      this.pointers.delete(s.pointerId);
      this.toIdle();
      return;
    }
    if (this.state === GestureStateName.DRAGGING) {
      this.pointers.delete(s.pointerId);
      this.releaseHeldButton(out);
      this.toIdle();
      return;
    }
    if (this.state === GestureStateName.HOLD_MULTI) {
      this.pointers.delete(s.pointerId);
      if (this.pointers.size === 0) {
        this.releaseHeldButton(out); // 全部抬起才释放引擎持有的左键
        this.toIdle();
      }
      return;
    }
    if (this.state === GestureStateName.TWO_PENDING) {
      const dt: number = s.tMs - tr.downTMs;
      const disp: number = Math.hypot(tr.lastX - tr.startX, tr.lastY - tr.startY);
      const thisOk: boolean = dt <= this.cfg.tapMaxMs && disp <= this.cfg.tapMaxVp;
      this.pointers.delete(s.pointerId);
      if (this.pointers.size === 0) {
        if (this.tapCandidate && this.tapFirstLiftOk && thisOk) {
          out.push(cmdButton(HidButton.RIGHT, true));
          out.push(cmdButton(HidButton.RIGHT, false));
        }
        this.toIdle();
      } else {
        this.tapFirstLiftOk = thisOk;
        if (!thisOk) {
          this.tapCandidate = false; // 先抬指出局：右键轻点作废，余指仅可滚动/静默抬起
        }
      }
      return;
    }
    if (this.state === GestureStateName.SCROLLING) {
      this.pointers.delete(s.pointerId);
      if (this.pointers.size === 0) {
        // 末指抬起：按末速起滑（惯性滑行），无资格则冲账余量收尾
        this.startFling();
        if (this.flingV === 0) {
          this.flushScroll(out);
        }
        this.toIdle();
      }
      // 还有手指：滚动继续（另一指接管）
      return;
    }
    if (this.state === GestureStateName.THREE_PENDING) {
      this.pointers.delete(s.pointerId);
      if (this.pointers.size < 3) {
        this.rebaseAfterThree();
      }
    }
  }

  // 光标命令：帧内位移 → 加速换算，亚像素残差留存引擎下次累计。
  private emitCursor(prevX: number, prevY: number, prevT: number, s: TouchSample, out: GestureCommand[]): void {
    const dxVp: number = s.x - prevX;
    const dyVp: number = s.y - prevY;
    const dt: number = s.tMs - prevT;
    const r: AccelResult = toCounts(dxVp, dyVp, dt, this.cfg, this.cursorResX, this.cursorResY);
    this.cursorResX = r.resX;
    this.cursorResY = r.resY;
    if (r.dx !== 0 || r.dy !== 0) {
      out.push(cmdCursorMove(r.dx, r.dy));
    }
  }

  // 滚动累积：手指垂直位移 × 单位换算进累积器，整数部分即时发出；
  // 同时用指数平滑估计速度（抬指惯性滑行的初速来源，抖动不进初速）。
  // 自然滚动：手指下滑（+dy）→ 内容随手指 → 视图上滚 → HI_RES 正值；非自然取反。
  private accumulateScroll(dyVp: number, dtMs: number, out: GestureCommand[]): void {
    if (dtMs > 0) {
      const inst: number = dyVp / dtMs;
      this.scrollVelVp = this.scrollVelVp * (1 - SCROLL_VELOCITY_ALPHA) + inst * SCROLL_VELOCITY_ALPHA;
    }
    this.scrollByVp(dyVp, out);
  }

  // 位移 → HI_RES 累积器（含自然滚动符号），整数部分即时发出，余量留待冲账。
  private scrollByVp(dyVp: number, out: GestureCommand[]): void {
    const sign: number = this.cfg.naturalScroll ? 1 : -1;
    this.scrollAccUnits += dyVp * this.cfg.scrollUnitsPerPixel * sign;
    const emit: number = Math.trunc(this.scrollAccUnits);
    if (emit !== 0) {
      this.scrollAccUnits -= emit;
      out.push(cmdScroll(emit));
    }
  }

  // 抬指起滑：末速低于阈值不起滑；超上限截断（防甩飞）。
  private startFling(): void {
    const v: number = this.scrollVelVp;
    this.scrollVelVp = 0;
    if (Math.abs(v) < this.cfg.flingMinVpPerMs) {
      return;
    }
    const max: number = this.cfg.flingMaxVpPerMs;
    this.flingV = Math.max(-max, Math.min(max, v));
    this.flingElapsedMs = 0;
  }

  // 结束惯性并冲账余量（触停/超时/低于阈值/取消共用）。
  private stopFling(out: GestureCommand[]): void {
    this.flingV = 0;
    this.flingElapsedMs = 0;
    this.flushScroll(out);
  }

  // 结束/多指接管前冲账：累积余量（|余量|<1）四舍五入成最后一条 Scroll，不得丢尾。
  private flushScroll(out: GestureCommand[]): void {
    const tail: number = Math.round(this.scrollAccUnits);
    this.scrollAccUnits = 0;
    if (tail !== 0) {
      out.push(cmdScroll(tail));
    }
  }

  // 多指回落：存活指 <3 时起点重置为当前位置，转 TWO_PENDING（无轻点资格，防回落即误触右键）。
  private rebaseAfterThree(): void {
    this.pointers.forEach((tr: PointerTrack) => {
      tr.startX = tr.lastX;
      tr.startY = tr.lastY;
      tr.downTMs = tr.lastTMs;
    });
    this.state = GestureStateName.TWO_PENDING;
    this.tapCandidate = false;
    this.tapFirstLiftOk = false;
  }

  // CANCEL 红线：释放引擎持有的按键、终止惯性并冲账滚动余量、清空全部状态回 IDLE，禁止残留。
  private cancelAll(out: GestureCommand[]): void {
    this.releaseHeldButton(out);
    this.stopFling(out); // 含余量冲账
    this.toIdle();
  }

  private releaseHeldButton(out: GestureCommand[]): void {
    if (this.buttonHeld) {
      this.buttonHeld = false;
      out.push(cmdButton(HidButton.LEFT, false));
    }
  }

  private toIdle(): void {
    this.state = GestureStateName.IDLE;
    this.primaryId = -1;
    this.tapCandidate = false;
    this.tapFirstLiftOk = false;
    this.tapDragArmed = false;
    this.buttonHeld = false;
    this.cursorResX = 0;
    this.cursorResY = 0;
    this.scrollVelVp = 0;
    // 惯性滑行跨 IDLE 存活（末指抬起后引擎即回 IDLE，滑行由 tick 继续驱动）：
    // 滑行中不得清累积器（余量要留到 stopFling 冲账）
    if (this.flingV === 0) {
      this.scrollAccUnits = 0;
    }
    this.pointers.clear();
  }

  // 轻点窗口是否已过（以首指按下时刻为基准，惰性比较）。
  private tapWindowExpired(tMs: number): boolean {
    const primary: PointerTrack | undefined = this.pointers.get(this.primaryId);
    if (primary === undefined) {
      return true;
    }
    return tMs - primary.downTMs > this.cfg.tapMaxMs;
  }

  private anyPointerOverTapDist(): boolean {
    let over: boolean = false;
    this.pointers.forEach((tr: PointerTrack) => {
      if (Math.hypot(tr.lastX - tr.startX, tr.lastY - tr.startY) > this.cfg.tapMaxVp) {
        over = true;
      }
    });
    return over;
  }
}
