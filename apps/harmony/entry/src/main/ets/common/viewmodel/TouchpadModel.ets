// 触摸板 ViewModel（FR-1）：UI 触摸事件 → TouchSample 流 → GestureEngine → Transport。
// 触摸数据消费规则（PRD §4.2，铁律）：
// - 一律消费 event.touches（按器件报点率上报）而非 changedTouches（按屏幕刷新率重采样）；
// - 用 event.getHistoricalPoints() 取回帧内被合并报点，逐点 feed（否则快速滑动丢位移、双指滚动起始抖动）；
//   文档查证（ts-universal-events-touch）：TouchEvent.getHistoricalPoints(): Array<HistoricalPoint>，
//   HistoricalPoint.touchObject: TouchObject（id/x/y，vp）；API 不提供逐点时间戳，
//   故帧内历史点时间在上一事件与当前事件时间戳间线性内插。
// - tMs 取 BaseEvent.timestamp（系统启动起 ns，单调），/1e6 转毫秒喂引擎。
// Up/Cancel 时被抬手指只存在于 changedTouches（touches 已不含该指），位置取 changed 值。
import { GestureEngine, GestureCommandType } from '../gesture/GestureEngine';
import type { GestureCommand } from '../gesture/GestureEngine';
import { TouchKind } from '../gesture/TouchSample';
import type { TouchSample } from '../gesture/TouchSample';
import { defaultGestureConfig } from '../gesture/GestureConfig';
import type { GestureConfig } from '../gesture/GestureConfig';
import { newButton } from '../proto/Packet';
import type { Transport } from '../net/Transport';

export class TouchpadModel {
  private cfg: GestureConfig = defaultGestureConfig();
  private engine: GestureEngine = new GestureEngine(this.cfg);
  private transport: Transport | null = null;
  // 活跃指集合（DOWN 登记 / UP 注销 / CANCEL 清空），供 touches 流的指位升格判定。
  private activeIds: Set<number> = new Set<number>();
  private lastEventTms: number = 0;

  // 调试统计：触摸采样率 / 发送速率（每秒滚动窗口）。
  private sampleSecStart: number = 0;
  private samplesInSec: number = 0;
  private sampleRate: number = 0;
  private sendSecStart: number = 0;
  private sendsInSec: number = 0;
  private sendRate: number = 0;

  public bindTransport(t: Transport): void {
    this.transport = t;
  }

  // 设置页实时生效：直接改引擎持有的同一配置对象（引擎每拍读 this.cfg，见 GestureEngine）。
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

  // 主控页 onTouch 入口。
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
      // TouchType.Cancel（Home 返回、折叠切换等）：引擎任意 CANCEL 即全局复位
      this.feedSample(0, 0, 0, TouchKind.CANCEL, tms);
    }
    this.lastEventTms = tms;
  }

  // 切页/退出主控页时调用：CANCEL 复位引擎（释放按住的左键、冲账滚动余量），不残留。
  public cancelGesture(): void {
    this.feedSample(0, 0, 0, TouchKind.CANCEL, new Date().getTime());
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

  // 命令分发：MOVE/SCROLL→UDP（自然滚动已在引擎内换算，此处不二次取反）；BUTTON→TCP。
  private dispatch(cmds: GestureCommand[]): void {
    if (cmds.length === 0) {
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
