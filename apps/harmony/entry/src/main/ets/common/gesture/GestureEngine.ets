// 触摸板手势判定状态机（PRD §4.2 手势表 / §4.3 三指切换）。
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
  public static readonly SWITCH_TAB: number = 3;
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
  direction: number; // SWITCH_TAB：位移符号 -1 | +1
}

export function cmdCursorMove(dx: number, dy: number): GestureCommand {
  const c: GestureCommand = { type: GestureCommandType.CURSOR_MOVE, dx: dx, dy: dy, btn: 0, down: false, dyHiRes: 0, direction: 0 };
  return c;
}

export function cmdButton(btn: number, down: boolean): GestureCommand {
  const c: GestureCommand = { type: GestureCommandType.BUTTON, dx: 0, dy: 0, btn: btn, down: down, dyHiRes: 0, direction: 0 };
  return c;
}

export function cmdScroll(dyHiRes: number): GestureCommand {
  const c: GestureCommand = { type: GestureCommandType.SCROLL, dx: 0, dy: 0, btn: 0, down: false, dyHiRes: dyHiRes, direction: 0 };
  return c;
}

export function cmdSwitchTab(direction: number): GestureCommand {
  const c: GestureCommand = { type: GestureCommandType.SWITCH_TAB, dx: 0, dy: 0, btn: 0, down: false, dyHiRes: 0, direction: direction };
  return c;
}

// 状态机状态。注释含各状态的进入/退出条件。
export class GestureStateName {
  // 无接触。
  // 进入：任何复位路径（手势完成、CANCEL、全部抬起）。
  // 退出：首个 DOWN → SINGLE_PENDING。
  public static readonly IDLE: number = 0;
  // 单指按下待定型：轻点/长按候选（未超 150ms、位移未超 8vp）。
  // 进入：IDLE 下单指 DOWN。
  // 退出：UP ≤150ms 且 ≤8vp → 成对发左键 down+up 回 IDLE；UP 在 150..300ms 之间 → 无命令回 IDLE；
  //       惰性到达 ≥300ms 且 ≤8vp → DRAGGING（发左键 down）；位移超 8vp → CURSOR；第二指 DOWN → TWO_PENDING；
  //       CANCEL → IDLE。
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
  // ≥3 指存活：仅水平同向位移各 ≥60vp 触发 SwitchTab（垂直不触发——系统三指下滑为截屏，不拦截不响应）；
  // 500ms 节流，窗口内二次触发忽略；触发后重置起点供连续滑动。
  // 进入：任一状态存活指达 3（SCROLLING 先冲账；DRAGGING/HOLD_MULTI 不进入，转 HOLD_MULTI 保持按键）。
  // 退出：存活指 <3 → 回落 TWO_PENDING（起点重置、无轻点资格）；CANCEL → IDLE。
  public static readonly THREE_PENDING: number = 7;
}

// 指针跟踪记录（引擎内部）。
interface PointerTrack {
  startX: number; // 本段判定的起点（触发切换后重置）
  startY: number;
  lastX: number;
  lastY: number;
  downTMs: number; // 本段起点时间
  lastTMs: number;
}

export class GestureEngine {
  private cfg: GestureConfig;
  private state: number = GestureStateName.IDLE;
  private pointers: Map<number, PointerTrack> = new Map();
  private primaryId: number = -1; // 首指（轻点窗口/按下时间差的基准）
  private tapCandidate: boolean = false; // 双指轻点资格（按下时间差 ≤40ms）
  private tapFirstLiftOk: boolean = false; // 双指中先抬指的轻点资格
  private buttonHeld: boolean = false; // 引擎持有的左键（长按拖拽）
  private cursorResX: number = 0; // 光标亚像素残差
  private cursorResY: number = 0;
  private scrollAccUnits: number = 0; // 滚动累积器（HI_RES 单位，含小数）
  private lastSwitchTMs: number = 0; // 上次三指切换时刻（节流跨手势生效）
  private hasSwitched: boolean = false;

  constructor(cfg: GestureConfig) {
    this.cfg = cfg;
  }

  // 当前状态（调试面板展示 / 单测断言用）。
  public currentState(): number {
    return this.state;
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
    const tr: PointerTrack = { startX: s.x, startY: s.y, lastX: s.x, lastY: s.y, downTMs: s.tMs, lastTMs: s.tMs };
    if (this.state === GestureStateName.IDLE) {
      this.pointers.set(s.pointerId, tr);
      this.primaryId = s.pointerId;
      this.state = GestureStateName.SINGLE_PENDING;
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
      this.flushScroll(out); // 三指接管前冲账滚动余量，不丢尾
    }
    if (this.pointers.size >= 3) {
      // 第三指插入：取消此前一切未定型判定（尚无按键按下，无命令可发）
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
        this.accumulateScroll(s.y - prevY, out);
      }
      return;
    }
    if (this.state === GestureStateName.SCROLLING) {
      this.accumulateScroll(s.y - prevY, out);
      return;
    }
    if (this.state === GestureStateName.THREE_PENDING) {
      this.evalSwitch(s.tMs, out);
    }
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
        this.flushScroll(out); // 末指抬起：冲账累积余量，不丢尾
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

  // 滚动累积：手指垂直位移 × 单位换算进累积器，整数部分即时发出。
  // 自然滚动：手指下滑（+dy）→ 内容随手指 → 视图上滚 → HI_RES 正值；非自然取反。
  private accumulateScroll(dyVp: number, out: GestureCommand[]): void {
    const sign: number = this.cfg.naturalScroll ? 1 : -1;
    this.scrollAccUnits += dyVp * this.cfg.scrollUnitsPerPixel * sign;
    const emit: number = Math.trunc(this.scrollAccUnits);
    if (emit !== 0) {
      this.scrollAccUnits -= emit;
      out.push(cmdScroll(emit));
    }
  }

  // 结束/三指接管前冲账：累积余量（|余量|<1）四舍五入成最后一条 Scroll，不得丢尾。
  private flushScroll(out: GestureCommand[]): void {
    const tail: number = Math.round(this.scrollAccUnits);
    this.scrollAccUnits = 0;
    if (tail !== 0) {
      out.push(cmdScroll(tail));
    }
  }

  // 三指切换判定：≥3 指存活、水平位移同号且各 ≥threeFingerSwipeVp；垂直位移不触发；
  // swipeThrottleMs 窗口内二次触发忽略。触发后重置各指起点供连续滑动。
  private evalSwitch(tMs: number, out: GestureCommand[]): void {
    if (this.pointers.size < 3) {
      return;
    }
    let dir: number = 0;
    let ok: boolean = true;
    this.pointers.forEach((tr: PointerTrack) => {
      const hx: number = tr.lastX - tr.startX;
      if (Math.abs(hx) < this.cfg.threeFingerSwipeVp) {
        ok = false;
        return;
      }
      const sgn: number = hx > 0 ? 1 : -1;
      if (dir === 0) {
        dir = sgn;
      } else if (dir !== sgn) {
        ok = false;
      }
    });
    if (!ok || dir === 0) {
      return;
    }
    if (this.hasSwitched && tMs - this.lastSwitchTMs < this.cfg.swipeThrottleMs) {
      return; // 节流窗口内忽略（起点不重置，窗口后可凭累计位移继续触发）
    }
    this.hasSwitched = true;
    this.lastSwitchTMs = tMs;
    out.push(cmdSwitchTab(dir));
    this.pointers.forEach((tr: PointerTrack) => {
      tr.startX = tr.lastX;
      tr.startY = tr.lastY;
    });
  }

  // 三指回落：存活指 <3 时起点重置为当前位置，转 TWO_PENDING（无轻点资格，防回落即误触右键）。
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

  // CANCEL 红线：释放引擎持有的按键、冲账滚动余量、清空全部状态回 IDLE，禁止残留。
  private cancelAll(out: GestureCommand[]): void {
    this.releaseHeldButton(out);
    this.flushScroll(out);
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
    this.buttonHeld = false;
    this.cursorResX = 0;
    this.cursorResY = 0;
    this.scrollAccUnits = 0;
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
