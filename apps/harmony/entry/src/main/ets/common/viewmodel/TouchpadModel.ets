// 触摸板 ViewModel：UI 触摸事件 → TouchSample 流 → GestureEngine → Transport。
// 触摸数据消费必须用 event.touches + getHistoricalPoints()：changedTouches 按屏幕刷新率重采样，会丢
// 帧内合并报点；历史点无时间戳，按上一事件与当前事件间隔线性内插。Up/Cancel 的被抬指只在
// changedTouches（touches 已不含该指）。tMs 取 BaseEvent.timestamp（ns，单调）/1e6。
import { GestureEngine, GestureCommandType } from '../gesture/GestureEngine';
import type { GestureCommand } from '../gesture/GestureEngine';
import { TouchKind } from '../gesture/TouchSample';
import type { TouchSample } from '../gesture/TouchSample';
import { defaultGestureConfig } from '../gesture/GestureConfig';
import type { GestureConfig } from '../gesture/GestureConfig';
import { newButton } from '../proto/Packet';
import type { Transport } from '../net/Transport';

// ≈60Hz：与屏幕刷新同量级，滚动步进才跟得上衰减曲线。
const FLING_TICK_MS: number = 16;

export class TouchpadModel {
  private cfg: GestureConfig = defaultGestureConfig();
  private engine: GestureEngine = new GestureEngine(this.cfg);
  private transport: Transport | null = null;
  // 活跃指集合（DOWN 登记 / UP 注销 / CANCEL 清空），供指位升格判定。
  private activeIds: Set<number> = new Set<number>();
  private lastEventTms: number = 0;
  // 时钟留在本层：触摸样本用系统启动时间、定时器用 Date.now()，两者不同基，
  // 故引擎只认「距上次 tick 的增量」。
  private flingTimer: number = -1;
  private flingWallMs: number = 0;

  private sampleSecStart: number = 0;
  private samplesInSec: number = 0;
  private sampleRate: number = 0;
  private sendSecStart: number = 0;
  private sendsInSec: number = 0;
  private sendRate: number = 0;

  public bindTransport(t: Transport): void {
    this.transport = t;
  }

  // 设置页实时生效：直接改引擎持有的同一配置对象（引擎每拍读 this.cfg）。
  public applySettings(sensitivity: number, naturalScroll: boolean): void {
    this.cfg.sensitivity = sensitivity;
    this.cfg.naturalScroll = naturalScroll;
  }

  public currentState(): number {
    return this.engine.currentState();
  }

  public getSampleRate(): number {
    return this.sampleRate;
  }

  public getSendRate(): number {
    return this.sendRate;
  }

  public onTouch(ev: TouchEvent): void {
    const tms: number = ev.timestamp / 1000000; // ns → ms（单调时钟）
    const span: number = tms - this.lastEventTms;
    const hist: Array<HistoricalPoint> = ev.getHistoricalPoints();
    for (let i = 0; i < hist.length; i++) {
      const t: TouchObject = hist[i].touchObject;
      const histTms: number = this.lastEventTms + span * (i + 1) / (hist.length + 1);
      this.feedSample(t.id, t.x, t.y, TouchKind.MOVE, histTms);
    }
    if (ev.type === TouchType.Down) {
      for (const t of ev.touches) {
        if (!this.activeIds.has(t.id)) {
          this.feedSample(t.id, t.x, t.y, TouchKind.DOWN, tms);
        }
      }
    } else if (ev.type === TouchType.Move) {
      for (const t of ev.touches) {
        // 未知指升格为 DOWN 自愈（帧合并极端情况下 Down 丢失，防该指永久失联）
        const known: boolean = this.activeIds.has(t.id);
        this.feedSample(t.id, t.x, t.y, known ? TouchKind.MOVE : TouchKind.DOWN, tms);
      }
    } else if (ev.type === TouchType.Up) {
      for (const t of ev.changedTouches) {
        this.feedSample(t.id, t.x, t.y, TouchKind.UP, tms);
      }
    } else if (ev.type === TouchType.Cancel) {
      // TouchType.Cancel（Home 返回、折叠切换等）：任意 CANCEL 即全局复位
      this.feedSample(0, 0, 0, TouchKind.CANCEL, tms);
    }
    this.lastEventTms = tms;
  }

  // 切页/退出主控页时调用：CANCEL 复位引擎（释放按键、冲账滚动、终止惯性），不残留。
  public cancelGesture(): void {
    this.feedSample(0, 0, 0, TouchKind.CANCEL, new Date().getTime());
    this.stopFlingTimer();
  }

  private feedSample(pointerId: number, x: number, y: number, kind: number, tMs: number): void {
    if (kind === TouchKind.DOWN) {
      this.activeIds.add(pointerId);
    } else if (kind === TouchKind.UP) {
      this.activeIds.delete(pointerId);
    } else if (kind === TouchKind.CANCEL) {
      this.activeIds.clear();
    }
    const s: TouchSample = { pointerId: pointerId, x: x, y: y, kind: kind, tMs: tMs };
    const cmds: GestureCommand[] = this.engine.feed(s);
    this.noteSample(tMs);
    this.dispatch(cmds);
  }

  private syncFlingTimer(): void {
    if (!this.engine.hasFling()) {
      this.stopFlingTimer();
      return;
    }
    if (this.flingTimer >= 0) {
      return;
    }
    this.flingWallMs = new Date().getTime();
    this.flingTimer = setInterval(() => {
      const now: number = new Date().getTime();
      const dt: number = now - this.flingWallMs;
      this.flingWallMs = now;
      const cmds: GestureCommand[] = this.engine.tick(dt);
      if (!this.engine.hasFling()) {
        this.stopFlingTimer();
      }
      this.dispatch(cmds);
    }, FLING_TICK_MS);
  }

  private stopFlingTimer(): void {
    if (this.flingTimer >= 0) {
      clearInterval(this.flingTimer);
      this.flingTimer = -1;
    }
  }

  // 命令分发：MOVE/SCROLL→UDP（自然滚动已在引擎内换算，不二次取反）；BUTTON→TCP。
  private dispatch(cmds: GestureCommand[]): void {
    if (cmds.length === 0) {
      this.syncFlingTimer();
      return;
    }
    for (const c of cmds) {
      if (c.type === GestureCommandType.CURSOR_MOVE) {
        if (this.transport !== null) {
          this.transport.sendMove(c.dx, c.dy);
        }
        this.noteSend();
      } else if (c.type === GestureCommandType.SCROLL) {
        if (this.transport !== null) {
          this.transport.sendScroll(c.dyHiRes);
        }
        this.noteSend();
      } else if (c.type === GestureCommandType.BUTTON) {
        if (this.transport !== null) {
          this.transport.sendReliable(newButton(this.transport.nextSeq(), c.btn, c.down));
        }
        this.noteSend();
      }
    }
    this.syncFlingTimer();
  }

  private noteSample(tMs: number): void {
    if (tMs - this.sampleSecStart >= 1000) {
      this.sampleRate = this.samplesInSec;
      this.samplesInSec = 0;
      this.sampleSecStart = tMs;
    }
    this.samplesInSec++;
  }

  private noteSend(): void {
    const tMs: number = new Date().getTime();
    if (tMs - this.sendSecStart >= 1000) {
      this.sendRate = this.sendsInSec;
      this.sendsInSec = 0;
      this.sendSecStart = tMs;
    }
    this.sendsInSec++;
  }
}
