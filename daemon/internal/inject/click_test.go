package inject

import (
	"testing"
	"time"
)

// 双击合成的判定表：同键、限时、限距的连续按下才递增点击序号。
func TestClickTrackerSequence(t *testing.T) {
	base := time.Unix(1000, 0)
	tests := []struct {
		name  string
		steps func(c *clickTracker) []PointerState
		want  []int
	}{
		{
			name: "两次快速同位置按下 → 双击",
			steps: func(c *clickTracker) []PointerState {
				return []PointerState{
					c.press(1, base),
					c.release(1),
					c.press(1, base.Add(120*time.Millisecond)),
				}
			},
			want: []int{1, 1, 2},
		},
		{
			name: "三次快速同位置按下 → 三击封顶",
			steps: func(c *clickTracker) []PointerState {
				return []PointerState{
					c.press(1, base),
					c.release(1),
					c.press(1, base.Add(100*time.Millisecond)),
					c.release(1),
					c.press(1, base.Add(200*time.Millisecond)),
					c.release(1),
					c.press(1, base.Add(300*time.Millisecond)),
				}
			},
			want: []int{1, 1, 2, 2, 3, 3, 3},
		},
		{
			name: "超过双击间隔 → 重置为单击",
			steps: func(c *clickTracker) []PointerState {
				return []PointerState{
					c.press(1, base),
					c.release(1),
					c.press(1, base.Add(DefaultDoubleClickInterval+time.Millisecond)),
				}
			},
			want: []int{1, 1, 1},
		},
		{
			name: "指针位移超阈值 → 重置为单击",
			steps: func(c *clickTracker) []PointerState {
				return []PointerState{
					c.press(1, base),
					c.release(1),
					func() PointerState { c.moved(20, 0); return c.state() }(),
					c.press(1, base.Add(100*time.Millisecond)),
				}
			},
			want: []int{1, 1, 1, 1},
		},
		{
			name: "位移在阈值内 → 仍算双击",
			steps: func(c *clickTracker) []PointerState {
				return []PointerState{
					c.press(1, base),
					c.release(1),
					func() PointerState { c.moved(3, 4); return c.state() }(), // 5 < 8
					c.press(1, base.Add(100*time.Millisecond)),
				}
			},
			want: []int{1, 1, 1, 2},
		},
		{
			name: "换按钮打断序列",
			steps: func(c *clickTracker) []PointerState {
				return []PointerState{
					c.press(1, base),
					c.release(1),
					c.press(3, base.Add(50*time.Millisecond)),
					c.release(3),
					c.press(1, base.Add(100*time.Millisecond)),
				}
			},
			want: []int{1, 1, 1, 1, 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newClickTracker(0, 0) // 零值取默认阈值
			got := tc.steps(c)
			if len(got) != len(tc.want) {
				t.Fatalf("步数 = %d, want %d", len(got), len(tc.want))
			}
			for i, st := range got {
				if st.ClickCount != tc.want[i] {
					t.Errorf("第 %d 步 ClickCount = %d, want %d", i, st.ClickCount, tc.want[i])
				}
			}
		})
	}
}

func TestClickTrackerButtonsDown(t *testing.T) {
	c := newClickTracker(0, 0)
	if st := c.press(1, time.Unix(0, 0)); st.ButtonsDown != 1 {
		t.Fatalf("左键按下 ButtonsDown = %d, want 1", st.ButtonsDown)
	}
	if st := c.press(3, time.Unix(0, 0)); st.ButtonsDown != 5 {
		t.Fatalf("左+右按下 ButtonsDown = %d, want 5", st.ButtonsDown)
	}
	if st := c.release(1); st.ButtonsDown != 4 {
		t.Fatalf("左键抬起 ButtonsDown = %d, want 4", st.ButtonsDown)
	}
	if st := c.release(3); st.ButtonsDown != 0 {
		t.Fatalf("全抬起 ButtonsDown = %d, want 0", st.ButtonsDown)
	}
	// 抬起不改变点击序号（down/up 必须携带同一 clickState）
	c2 := newClickTracker(0, 0)
	c2.press(1, time.Unix(0, 0))
	c2.release(1)
	c2.press(1, time.Unix(0, int64(100*time.Millisecond)))
	if st := c2.release(1); st.ClickCount != 2 {
		t.Fatalf("双击抬起 ClickCount = %d, want 2", st.ClickCount)
	}
}

// Injector 必须把合成结果交给后端（darwin 写 kCGMouseEventClickState），
// 且位移帧携带当前按住状态（darwin 选 mouseDragged 事件类型）。
func TestInjectorForwardsPointerState(t *testing.T) {
	w := &fakeDeviceWriter{}
	in := NewInjector(w, Config{DoubleClickInterval: time.Hour}) // 拉长窗口便于稳定双击

	if err := in.Button(1, true); err != nil {
		t.Fatalf("Button down: %v", err)
	}
	if w.pointer.ClickCount != 1 || w.pointer.ButtonsDown != 1 {
		t.Fatalf("单击按下状态 = %+v, want {1 1}", w.pointer)
	}
	if err := in.Button(1, false); err != nil {
		t.Fatalf("Button up: %v", err)
	}
	if w.pointer.ClickCount != 1 || w.pointer.ButtonsDown != 0 {
		t.Fatalf("单击抬起状态 = %+v, want {1 0}", w.pointer)
	}
	if err := in.Button(1, true); err != nil {
		t.Fatalf("Button down #2: %v", err)
	}
	if w.pointer.ClickCount != 2 {
		t.Fatalf("第二次按下 ClickCount = %d, want 2", w.pointer.ClickCount)
	}
	// 拖拽位移帧：序号与按住位保持
	if err := in.Move(10, -4); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if w.pointer.ClickCount != 2 || w.pointer.ButtonsDown != 1 {
		t.Fatalf("拖拽帧状态 = %+v, want {2 1}", w.pointer)
	}
}
